package webhook

import (
	"net/http"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/snapp-incubator/contour-admission-webhook/internal/cache"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// checker defines the interface for validation checks.
type checker interface {
	check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr)
	setNext(checker)
}

// checkRequest holds all context needed for validation checks.
type checkRequest struct {
	newObj          *contourv1.HTTPProxy
	oldObj          *contourv1.HTTPProxy
	dryRun          bool
	cache           *cache.Cache
	newIngressClass *ingressClass
	oldIngressClass *ingressClass
}

// ingressClass represents parsed ingress class information.
type ingressClass struct {
	name  string
	valid bool
}

var (
	httpproxyResource = metav1.GroupVersionResource{
		Group:    "projectcontour.io",
		Version:  "v1",
		Resource: "httpproxies",
	}
)

// validateV1 validates an HTTPProxy admission request.
func validateV1(ar admissionv1.AdmissionReview, c *cache.Cache) (*admissionv1.AdmissionResponse, *httpErr) {
	if ar.Request.Resource != httpproxyResource {
		return nil, newHTTPError(http.StatusBadRequest,
			"invalid resource: expected %s, got %s", httpproxyResource, ar.Request.Resource)
	}

	httpproxy := &contourv1.HTTPProxy{}
	httpproxyOld := &contourv1.HTTPProxy{}

	// Decode new object (required for CREATE and UPDATE)
	if len(ar.Request.Object.Raw) > 0 {
		if _, _, err := deserializer.Decode(ar.Request.Object.Raw, nil, httpproxy); err != nil {
			return nil, newHTTPError(http.StatusBadRequest,
				"failed to decode object: %s", err.Error())
		}
	}

	// Decode old object (required for UPDATE and DELETE)
	if len(ar.Request.OldObject.Raw) > 0 {
		if _, _, err := deserializer.Decode(ar.Request.OldObject.Raw, nil, httpproxyOld); err != nil {
			return nil, newHTTPError(http.StatusBadRequest,
				"failed to decode old object: %s", err.Error())
		}
	}

	// Determine if this is a dry run
	dryRun := ar.Request.DryRun != nil && *ar.Request.DryRun

	cr := &checkRequest{
		newObj: httpproxy,
		oldObj: httpproxyOld,
		dryRun: dryRun,
		cache:  c,
	}

	switch ar.Request.Operation {
	case admissionv1.Create:
		return runCheckerChain(cr,
			&checkIngressClassNameOnCreate{},
			&checkFqdnOnCreate{},
		)

	case admissionv1.Update:
		return runCheckerChain(cr,
			&checkIngressClassNameOnUpdate{},
			&checkFqdnOnUpdate{},
		)

	case admissionv1.Delete:
		return runCheckerChain(cr,
			&checkIngressClassNameOnDelete{},
			&checkFqdnOnDelete{},
		)

	default:
		return nil, newHTTPError(http.StatusBadRequest,
			"unsupported operation: %s", ar.Request.Operation)
	}
}

// runCheckerChain builds and runs a chain of checkers.
func runCheckerChain(cr *checkRequest, checkers ...checker) (*admissionv1.AdmissionResponse, *httpErr) {
	if len(checkers) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}, nil
	}

	// Build chain
	for i := 0; i < len(checkers)-1; i++ {
		checkers[i].setNext(checkers[i+1])
	}

	// Run first checker (which will cascade through the chain)
	return checkers[0].check(cr)
}

// allowedResponse creates an allowed admission response.
func allowedResponse() *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{Allowed: true}
}

// deniedResponse creates a denied admission response with a message.
func deniedResponse(code int32, message string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Code:    code,
			Message: message,
		},
	}
}
