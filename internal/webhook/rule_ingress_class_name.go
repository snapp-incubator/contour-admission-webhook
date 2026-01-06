package webhook

import (
	"net/http"

	"github.com/snapp-incubator/contour-admission-webhook/pkg/utils"
	admissionv1 "k8s.io/api/admission/v1"
)

// checkIngressClassNameOnCreate validates ingressClassName for CREATE operations.
type checkIngressClassNameOnCreate struct {
	next checker
}

// checkIngressClassNameOnUpdate validates ingressClassName for UPDATE operations.
type checkIngressClassNameOnUpdate struct {
	next checker
}

// checkIngressClassNameOnDelete validates ingressClassName for DELETE operations.
type checkIngressClassNameOnDelete struct {
	next checker
}

func (c *checkIngressClassNameOnCreate) setNext(next checker) { c.next = next }
func (c *checkIngressClassNameOnUpdate) setNext(next checker) { c.next = next }
func (c *checkIngressClassNameOnDelete) setNext(next checker) { c.next = next }

func (c *checkIngressClassNameOnCreate) check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	ingressClassName := utils.GetIngressClassName(cr.newObj)

	if ingressClassName == "" {
		return deniedResponse(http.StatusBadRequest, "ingressClassName is not set"), nil
	}

	if !utils.ValidateIngressClassName(ingressClassName) {
		return deniedResponse(http.StatusBadRequest, "ingressClassName is not valid"), nil
	}

	cr.newIngressClass = &ingressClass{
		name:  ingressClassName,
		valid: true,
	}

	return c.passToNext(cr)
}

func (c *checkIngressClassNameOnCreate) passToNext(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	if c.next != nil {
		return c.next.check(cr)
	}
	return allowedResponse(), nil
}

func (c *checkIngressClassNameOnUpdate) check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	newIngressClassName := utils.GetIngressClassName(cr.newObj)

	if newIngressClassName == "" {
		return deniedResponse(http.StatusBadRequest, "ingressClassName is not set"), nil
	}

	if !utils.ValidateIngressClassName(newIngressClassName) {
		return deniedResponse(http.StatusBadRequest, "ingressClassName is not valid"), nil
	}

	oldIngressClassName := utils.GetIngressClassName(cr.oldObj)
	isOldValid := utils.ValidateIngressClassName(oldIngressClassName)

	cr.newIngressClass = &ingressClass{
		name:  newIngressClassName,
		valid: true,
	}
	cr.oldIngressClass = &ingressClass{
		name:  oldIngressClassName,
		valid: isOldValid,
	}

	return c.passToNext(cr)
}

func (c *checkIngressClassNameOnUpdate) passToNext(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	if c.next != nil {
		return c.next.check(cr)
	}
	return allowedResponse(), nil
}

func (c *checkIngressClassNameOnDelete) check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	oldIngressClassName := utils.GetIngressClassName(cr.oldObj)
	isOldValid := utils.ValidateIngressClassName(oldIngressClassName)

	cr.oldIngressClass = &ingressClass{
		name:  oldIngressClassName,
		valid: isOldValid,
	}

	return c.passToNext(cr)
}

func (c *checkIngressClassNameOnDelete) passToNext(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr) {
	if c.next != nil {
		return c.next.check(cr)
	}
	return allowedResponse(), nil
}
