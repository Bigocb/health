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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SmokeTestType defines the type of smoke test
// +kubebuilder:validation:Enum=dns;http;tcp
type SmokeTestType string

const (
	DNSTest  SmokeTestType = "dns"
	HTTPTest SmokeTestType = "http"
	TCPTest  SmokeTestType = "tcp"
)

// Severity defines the severity level of a test
// +kubebuilder:validation:Enum=critical;high;medium;low
type Severity string

const (
	CriticalSeverity Severity = "critical"
	HighSeverity     Severity = "high"
	MediumSeverity   Severity = "medium"
	LowSeverity      Severity = "low"
)

// TestStatus represents the result status of a test
// +kubebuilder:validation:Enum=pass;fail;timeout;unknown
type TestStatus string

const (
	PassStatus    TestStatus = "pass"
	FailStatus    TestStatus = "fail"
	TimeoutStatus TestStatus = "timeout"
	UnknownStatus TestStatus = "unknown"
)

// SmokeTestSpec defines the desired state of a SmokeTest
type SmokeTestSpec struct {
	// Type specifies the type of smoke test (dns, http, tcp)
	// +kubebuilder:validation:Required
	Type SmokeTestType `json:"type"`

	// Enabled indicates whether this test should be run
	// +kubebuilder:validation:Required
	// +kubebuilder:default:=true
	Enabled bool `json:"enabled"`

	// Severity indicates the importance of this test
	// +kubebuilder:default:=high
	Severity Severity `json:"severity,omitempty"`

	// Timeout specifies the timeout for the test (e.g., "5s", "30s")
	// +kubebuilder:default:="10s"
	Timeout string `json:"timeout,omitempty"`

	// Interval specifies how often to run the test (e.g., "1m", "5m")
	// Only used if controller runs tests continuously
	// +kubebuilder:default:="1m"
	Interval string `json:"interval,omitempty"`

	// --- DNS Test Fields ---

	// Domain is the domain to resolve (for DNS tests)
	Domain string `json:"domain,omitempty"`

	// --- HTTP Test Fields ---

	// URL is the URL to test (for HTTP tests)
	URL string `json:"url,omitempty"`

	// Method is the HTTP method to use (default: GET)
	// +kubebuilder:validation:Enum=GET;POST;PUT;DELETE;HEAD;OPTIONS
	// +kubebuilder:default:=GET
	Method string `json:"method,omitempty"`

	// ExpectedStatus is the expected HTTP status code
	// +kubebuilder:validation:Minimum:=100
	// +kubebuilder:validation:Maximum:=599
	ExpectedStatus int `json:"expectedStatus,omitempty"`

	// TLSInsecure allows insecure HTTPS connections
	// +kubebuilder:default:=false
	TLSInsecure bool `json:"tlsInsecure,omitempty"`

	// Headers are custom headers to include in the HTTP request
	Headers map[string]string `json:"headers,omitempty"`

	// UseServiceAccountToken enables automatic use of the pod's service account token
	// for authentication to the Kubernetes API
	// +kubebuilder:default:=false
	UseServiceAccountToken bool `json:"useServiceAccountToken,omitempty"`

	// --- TCP Test Fields ---

	// Host is the host to connect to (for TCP tests)
	Host string `json:"host,omitempty"`

	// Port is the port to connect to (for TCP tests)
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=65535
	Port int `json:"port,omitempty"`
}

// SmokeTestStatus defines the observed state of a SmokeTest
type SmokeTestStatus struct {
	// LastRun is the timestamp of the last test run
	LastRun *metav1.Time `json:"lastRun,omitempty"`

	// LastStatus is the result of the most recent test run
	LastStatus TestStatus `json:"lastStatus,omitempty"`

	// LastMessage contains details about the last test run
	LastMessage string `json:"lastMessage,omitempty"`

	// PassCount is the number of consecutive passes
	PassCount int `json:"passCount,omitempty"`

	// FailCount is the number of consecutive failures
	FailCount int `json:"failCount,omitempty"`

	// LastDuration is the duration of the last test in milliseconds
	LastDuration int `json:"lastDuration,omitempty"`
}

// SmokeTest is the Schema for the smoketests API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=st;plural=smoketests
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.lastStatus`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type SmokeTest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SmokeTestSpec   `json:"spec,omitempty"`
	Status SmokeTestStatus `json:"status,omitempty"`
}

// SmokeTestList contains a list of SmokeTest
// +kubebuilder:object:root=true
type SmokeTestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SmokeTest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SmokeTest{}, &SmokeTestList{})
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SmokeTest) DeepCopyInto(out *SmokeTest) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SmokeTest.
func (in *SmokeTest) DeepCopy() *SmokeTest {
	if in == nil {
		return nil
	}
	out := new(SmokeTest)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SmokeTest) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SmokeTestList) DeepCopyInto(out *SmokeTestList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SmokeTest, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SmokeTestList.
func (in *SmokeTestList) DeepCopy() *SmokeTestList {
	if in == nil {
		return nil
	}
	out := new(SmokeTestList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SmokeTestList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SmokeTestSpec) DeepCopyInto(out *SmokeTestSpec) {
	*out = *in
	if in.Headers != nil {
		in, out := &in.Headers, &out.Headers
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SmokeTestSpec.
func (in *SmokeTestSpec) DeepCopy() *SmokeTestSpec {
	if in == nil {
		return nil
	}
	out := new(SmokeTestSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SmokeTestStatus) DeepCopyInto(out *SmokeTestStatus) {
	*out = *in
	if in.LastRun != nil {
		in, out := &in.LastRun, &out.LastRun
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SmokeTestStatus.
func (in *SmokeTestStatus) DeepCopy() *SmokeTestStatus {
	if in == nil {
		return nil
	}
	out := new(SmokeTestStatus)
	in.DeepCopyInto(out)
	return out
}
