/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	healthv1alpha1 "github.com/ArchipelagoAI/health-reporter/api/v1alpha1"
	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
)

// SmokeTestReconciler reconciles a SmokeTest object
type SmokeTestReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	TestRegistry smoke_tests.TestRegistry
}

// +kubebuilder:rbac:groups=health.archipelago.ai,resources=smoketests,verbs=get;list;watch
// +kubebuilder:rbac:groups=health.archipelago.ai,resources=smoketests/status,verbs=get;patch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic
func (r *SmokeTestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Get the SmokeTest resource
	var smokeTest healthv1alpha1.SmokeTest
	if err := r.Get(ctx, req.NamespacedName, &smokeTest); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If resource is being deleted, remove from registry
	if !smokeTest.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.TestRegistry.RemoveTest(req.Name); err != nil {
			log.Error(err, "failed to remove test from registry", "test", req.Name)
			return ctrl.Result{}, err
		}
		log.Info("removed test from registry", "test", req.Name)
		return ctrl.Result{}, nil
	}

	// Convert CRD to test config
	testConfig := r.smokeTestToConfig(&smokeTest, req.Namespace)

	// Create appropriate test runner based on type
	var testRunner smoke_tests.TestRunner

	if !smokeTest.Spec.Enabled {
		// Remove disabled tests from registry
		if err := r.TestRegistry.RemoveTest(req.Name); err != nil {
			log.Error(err, "failed to remove disabled test from registry", "test", req.Name)
		}
		log.Info("test disabled, removed from registry", "test", req.Name)
	} else {
		// Create appropriate test runner
		switch smokeTest.Spec.Type {
		case healthv1alpha1.DNSTest:
			testRunner = smoke_tests.NewDNSTest(testConfig)
		case healthv1alpha1.HTTPTest:
			testRunner = smoke_tests.NewHTTPTest(testConfig)
		case healthv1alpha1.TCPTest:
			testRunner = smoke_tests.NewTCPTest(testConfig)
		default:
			log.Error(nil, "unsupported test type", "type", smokeTest.Spec.Type)
			return ctrl.Result{}, fmt.Errorf("unsupported test type: %s", smokeTest.Spec.Type)
		}

		// Add to registry
		if err := r.TestRegistry.AddTest(req.Name, testRunner); err != nil {
			log.Error(err, "failed to add test to registry", "test", req.Name)
			return ctrl.Result{}, err
		}
		log.Info("added test to registry", "test", req.Name, "type", smokeTest.Spec.Type)
	}

	// Update status
	now := time.Now()
	smokeTest.Status.LastRun = &metav1.Time{Time: now}

	// Note: We don't run the test here - that's done by the health reporter
	// The controller just manages the registry

	if err := r.Status().Update(ctx, &smokeTest); err != nil {
		log.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// smokeTestToConfig converts a SmokeTest CRD to a TestConfig
func (r *SmokeTestReconciler) smokeTestToConfig(st *healthv1alpha1.SmokeTest, namespace string) *smoke_tests.TestConfig {
	timeout := 10 * time.Second
	if st.Spec.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(st.Spec.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	severity := "high"
	if st.Spec.Severity != "" {
		severity = string(st.Spec.Severity)
	}

	config := &smoke_tests.TestConfig{
		Name:                   fmt.Sprintf("%s/%s", namespace, st.Name),
		Type:                   string(st.Spec.Type),
		Enabled:                st.Spec.Enabled,
		Severity:               severity,
		Timeout:                timeout,
		Domain:                 st.Spec.Domain,
		URL:                    st.Spec.URL,
		Method:                 st.Spec.Method,
		ExpectedStatus:         st.Spec.ExpectedStatus,
		TLSInsecure:            st.Spec.TLSInsecure,
		Headers:                st.Spec.Headers,
		UseServiceAccountToken: st.Spec.UseServiceAccountToken,
		Host:                   st.Spec.Host,
		Port:                   st.Spec.Port,
	}

	return config
}

// SetupWithManager sets up the controller with the Manager
func (r *SmokeTestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Filter for spec changes only
	predicate := predicate.GenerationChangedPredicate{}

	return ctrl.NewControllerManagedBy(mgr).
		For(&healthv1alpha1.SmokeTest{}).
		WithEventFilter(predicate).
		Complete(r)
}
