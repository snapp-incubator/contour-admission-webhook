package utils

import (
	"fmt"
	"slices"
	"sync"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/snapp-incubator/contour-admission-webhook/internal/config"
)

var (
	validIngressClasses     []string
	validIngressClassesOnce sync.Once
)

// GenerateCacheKey creates a cache key from ingressClassName and FQDN.
func GenerateCacheKey(ingressClassName, fqdn string) string {
	return fmt.Sprintf("%s/%s", ingressClassName, fqdn)
}

// GetIngressClassName extracts the ingress class name from an HTTPProxy.
// The kubernetes.io/ingress.class annotation takes precedence over the spec field.
// Returns empty string if httpproxy is nil.
func GetIngressClassName(httpproxy *contourv1.HTTPProxy) string {
	if httpproxy == nil {
		return ""
	}

	// Check annotation first for backwards compatibility
	if httpproxy.Annotations != nil {
		if annotation, found := httpproxy.Annotations["kubernetes.io/ingress.class"]; found && annotation != "" {
			return annotation
		}
	}

	return httpproxy.Spec.IngressClassName
}

// ValidateIngressClassName checks if the given ingressClassName is in the allowed list.
func ValidateIngressClassName(ingressClassName string) bool {
	if ingressClassName == "" {
		return false
	}

	loadValidIngressClasses()

	return slices.Contains(validIngressClasses, ingressClassName)
}

// loadValidIngressClasses loads the valid ingress classes from config.
// This is done once and cached for performance.
func loadValidIngressClasses() {
	validIngressClassesOnce.Do(func() {
		cfg := config.GetConfig()
		validIngressClasses = cfg.IngressClasses
	})
}

// ResetValidIngressClassesForTesting resets the cached valid ingress classes.
// This is intended for use in tests only.
func ResetValidIngressClassesForTesting() {
	validIngressClassesOnce = sync.Once{}
	validIngressClasses = nil
}
