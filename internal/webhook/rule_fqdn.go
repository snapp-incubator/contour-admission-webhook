package webhook

import (
	"fmt"
	"net/http"
	"time"

	"github.com/snapp-incubator/contour-admission-webhook/pkg/utils"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
)

// checkFqdnOnCreate validates FQDN uniqueness for CREATE operations.
type checkFqdnOnCreate struct {
	next checker
}

// checkFqdnOnUpdate validates FQDN uniqueness for UPDATE operations.
type checkFqdnOnUpdate struct {
	next checker
}

// checkFqdnOnDelete handles DELETE operations (no-op for FQDN).
type checkFqdnOnDelete struct {
	next checker
}

func (c *checkFqdnOnCreate) setNext(next checker) { c.next = next }
func (c *checkFqdnOnUpdate) setNext(next checker) { c.next = next }
func (c *checkFqdnOnDelete) setNext(next checker) { c.next = next }

func (c *checkFqdnOnCreate) check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	// No VirtualHost means no FQDN to validate
	if cr.newObj.Spec.VirtualHost == nil {
		return c.passToNext(cr)
	}

	// The ingressClass should be set by the previous checker
	if cr.newIngressClass == nil {
		return nil, newHTTPError(http.StatusInternalServerError, "ingressClass not initialized")
	}

	fqdn := cr.newObj.Spec.VirtualHost.Fqdn
	cacheKey := utils.GenerateCacheKey(cr.newIngressClass.name, fqdn)

	// Check if FQDN is already acquired
	if owner, exists := cr.cache.Get(cacheKey); exists {
		return deniedResponse(http.StatusForbidden,
			fmt.Sprintf("fqdn %q is already acquired by httpproxy %s/%s",
				fqdn, owner.Namespace, owner.Name)), nil
	}

	// Add to cache (with TTL) unless dry-run
	if !cr.dryRun {
		cr.cache.SetWithTTL(cacheKey,
			types.NamespacedName{Namespace: cr.newObj.Namespace, Name: cr.newObj.Name},
			entryTTL,
		)
	}

	return c.passToNext(cr)
}

func (c *checkFqdnOnCreate) passToNext(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	if c.next != nil {
		return c.next.check(cr)
	}
	return allowedResponse(), nil
}

func (c *checkFqdnOnUpdate) check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	newHasVH := cr.newObj.Spec.VirtualHost != nil
	oldHasVH := cr.oldObj.Spec.VirtualHost != nil

	// Neither old nor new has VirtualHost - nothing to do
	if !newHasVH && !oldHasVH {
		return c.passToNext(cr)
	}

	// Validate ingressClass is set
	if cr.newIngressClass == nil {
		return nil, newHTTPError(http.StatusInternalServerError, "newIngressClass not initialized")
	}
	if cr.oldIngressClass == nil {
		return nil, newHTTPError(http.StatusInternalServerError, "oldIngressClass not initialized")
	}

	// VirtualHost removed - nothing to validate (cleanup happens in controller)
	if !newHasVH && oldHasVH {
		return c.passToNext(cr)
	}

	newIngressClassName := cr.newIngressClass.name
	oldIngressClassName := cr.oldIngressClass.name

	// VirtualHost added - treat like CREATE
	if newHasVH && !oldHasVH {
		return c.handleVirtualHostAdded(cr, newIngressClassName)
	}

	// Both have VirtualHost - check if FQDN or ingressClass changed
	newFqdn := cr.newObj.Spec.VirtualHost.Fqdn
	oldFqdn := cr.oldObj.Spec.VirtualHost.Fqdn

	// No change in FQDN or ingressClass - nothing to do
	if newFqdn == oldFqdn && newIngressClassName == oldIngressClassName {
		return c.passToNext(cr)
	}

	// FQDN or ingressClass changed - validate new combination
	return c.handleFqdnChange(cr, newIngressClassName, newFqdn)
}

func (c *checkFqdnOnUpdate) handleVirtualHostAdded(cr *checkRequest, ingressClassName string) (*admissionv1.AdmissionResponse, *httpErr) {
	fqdn := cr.newObj.Spec.VirtualHost.Fqdn
	cacheKey := utils.GenerateCacheKey(ingressClassName, fqdn)

	if owner, exists := cr.cache.Get(cacheKey); exists {
		return deniedResponse(http.StatusForbidden,
			fmt.Sprintf("fqdn %q is already acquired by httpproxy %s/%s",
				fqdn, owner.Namespace, owner.Name)), nil
	}

	if !cr.dryRun {
		cr.cache.SetWithTTL(cacheKey,
			types.NamespacedName{Namespace: cr.newObj.Namespace, Name: cr.newObj.Name},
			entryTTL,
		)
	}

	return c.passToNext(cr)
}

func (c *checkFqdnOnUpdate) handleFqdnChange(cr *checkRequest, newIngressClassName, newFqdn string) (*admissionv1.AdmissionResponse, *httpErr) {
	newCacheKey := utils.GenerateCacheKey(newIngressClassName, newFqdn)

	// Check if new FQDN combination is already acquired
	if owner, exists := cr.cache.Get(newCacheKey); exists {
		return deniedResponse(http.StatusForbidden,
			fmt.Sprintf("fqdn %q is already acquired by httpproxy %s/%s",
				newFqdn, owner.Namespace, owner.Name)), nil
	}

	if !cr.dryRun {
		// Add new cache entry
		cr.cache.SetWithTTL(newCacheKey,
			types.NamespacedName{Namespace: cr.newObj.Namespace, Name: cr.newObj.Name},
			entryTTL,
		)
		// Note: Old entry cleanup is handled by the controller after successful update
	}

	return c.passToNext(cr)
}

func (c *checkFqdnOnUpdate) passToNext(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	if c.next != nil {
		return c.next.check(cr)
	}
	return allowedResponse(), nil
}

func (c *checkFqdnOnDelete) check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	// DELETE operations are always allowed for FQDN validation
	// Cache cleanup happens in the controller
	if c.next != nil {
		return c.next.check(cr)
	}
	return allowedResponse(), nil
}

// getExpirationTime calculates the expiration time for cache entries.
// Kept for backwards compatibility but use SetWithTTL instead.
func getExpirationTime() time.Time {
	return time.Now().Add(entryTTL)
}
