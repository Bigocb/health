#!/bin/bash
# Health Reporter Deployment Script
# Usage: ./deploy.sh [OPTIONS]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAMESPACE="monitoring"
RELEASE_NAME="health-reporter"
MODE="cron"
DRY_RUN=false
VERBOSE=false
DISCORD_WEBHOOK=""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Help text
show_help() {
    cat <<EOF
Health Reporter Deployment Script

Usage: $0 [OPTIONS]

Options:
    -h, --help              Show this help message
    -n, --namespace NS      Kubernetes namespace (default: monitoring)
    -r, --release NAME      Helm release name (default: health-reporter)
    -w, --webhook URL       Discord webhook URL (required)
    -m, --mimir URL         Mimir query URL (default: http://mimir-query:9009)
    -c, --cron              Deploy as CronJob (default)
    -d, --deployment        Deploy as Deployment (for testing)
    --dry-run              Show what would be deployed (no actual deploy)
    -v, --verbose          Enable verbose logging
    --uninstall            Uninstall the release

Examples:
    # Deploy as CronJob with Discord webhook
    $0 -w https://discord.com/api/webhooks/123/abc

    # Deploy as Deployment for testing
    $0 -d -w https://discord.com/api/webhooks/123/abc -v

    # Dry-run to see manifests
    $0 --dry-run -w https://discord.com/api/webhooks/123/abc

    # Uninstall
    $0 --uninstall
EOF
}

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
}

log_error() {
    echo -e "${RED}[✗]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[!]${NC} $1"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -r|--release)
            RELEASE_NAME="$2"
            shift 2
            ;;
        -w|--webhook)
            DISCORD_WEBHOOK="$2"
            shift 2
            ;;
        -m|--mimir)
            MIMIR_URL="$2"
            shift 2
            ;;
        -c|--cron)
            MODE="cron"
            shift
            ;;
        -d|--deployment)
            MODE="deployment"
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        --uninstall)
            log_info "Uninstalling $RELEASE_NAME from namespace $NAMESPACE..."
            if helm uninstall "$RELEASE_NAME" -n "$NAMESPACE" 2>/dev/null; then
                log_success "Release uninstalled successfully"
                exit 0
            else
                log_error "Failed to uninstall release (may not exist)"
                exit 1
            fi
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Validation
if [ -z "$DISCORD_WEBHOOK" ] && [ "$DRY_RUN" != "true" ]; then
    log_error "Discord webhook URL is required"
    echo "Use: $0 -w https://discord.com/api/webhooks/..."
    exit 1
fi

log_info "Validating prerequisites..."

# Check kubectl
if ! command -v kubectl &> /dev/null; then
    log_error "kubectl not found. Please install kubectl."
    exit 1
fi

# Check helm
if ! command -v helm &> /dev/null; then
    log_error "helm not found. Please install helm."
    exit 1
fi

# Check cluster connectivity
if ! kubectl cluster-info &> /dev/null; then
    log_error "Cannot connect to Kubernetes cluster"
    exit 1
fi

log_success "kubectl and helm found"

# Check namespace
log_info "Checking namespace: $NAMESPACE"
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    log_warning "Namespace $NAMESPACE does not exist. Creating..."
    kubectl create namespace "$NAMESPACE"
    log_success "Namespace created"
else
    log_success "Namespace exists"
fi

# Check Helm chart
log_info "Validating Helm chart..."
HELM_CHART_PATH="$SCRIPT_DIR/helm/health-reporter"

if [ ! -f "$HELM_CHART_PATH/Chart.yaml" ]; then
    log_error "Helm chart not found at $HELM_CHART_PATH"
    exit 1
fi

log_success "Helm chart found"

# Build Helm command
HELM_CMD="helm install $RELEASE_NAME $HELM_CHART_PATH -n $NAMESPACE"

# Add options
HELM_CMD="$HELM_CMD --set mode=$MODE"
HELM_CMD="$HELM_CMD --set discord.webhookUrl=$DISCORD_WEBHOOK"

if [ -n "$MIMIR_URL" ]; then
    HELM_CMD="$HELM_CMD --set mimir.url=$MIMIR_URL"
fi

if [ "$VERBOSE" = true ]; then
    HELM_CMD="$HELM_CMD --set logging.verbose=true"
fi

if [ "$DRY_RUN" = true ]; then
    HELM_CMD="$HELM_CMD --dry-run --debug"
fi

# Check if release already exists
if helm list -n "$NAMESPACE" | grep -q "^$RELEASE_NAME"; then
    log_warning "Release $RELEASE_NAME already exists. Will upgrade instead."
    HELM_CMD="helm upgrade $RELEASE_NAME $HELM_CHART_PATH -n $NAMESPACE"
    
    HELM_CMD="$HELM_CMD --set mode=$MODE"
    HELM_CMD="$HELM_CMD --set discord.webhookUrl=$DISCORD_WEBHOOK"
    
    if [ -n "$MIMIR_URL" ]; then
        HELM_CMD="$HELM_CMD --set mimir.url=$MIMIR_URL"
    fi
    
    if [ "$VERBOSE" = true ]; then
        HELM_CMD="$HELM_CMD --set logging.verbose=true"
    fi
    
    if [ "$DRY_RUN" = true ]; then
        HELM_CMD="$HELM_CMD --dry-run --debug"
    fi
fi

# Execute Helm command
log_info "Deploying health-reporter..."
log_info "Command: $HELM_CMD"
echo ""

if eval "$HELM_CMD"; then
    if [ "$DRY_RUN" = true ]; then
        log_success "Dry-run completed. Review the manifests above."
    else
        log_success "Deployment completed successfully"
        echo ""
        log_info "Next steps:"
        echo "  1. Check status: helm status $RELEASE_NAME -n $NAMESPACE"
        echo "  2. View logs: kubectl logs -f -n $NAMESPACE -l app=health-reporter"
        echo "  3. Trigger manually: kubectl create job -n $NAMESPACE --from=cronjob/$RELEASE_NAME-health-reporter health-reporter-manual-test"
    fi
else
    log_error "Deployment failed"
    exit 1
fi
