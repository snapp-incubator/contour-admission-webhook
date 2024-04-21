package webhook

import (
	"fmt"
	"net/http"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/snapp-incubator/contour-admission-webhook/internal/cache"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type checker interface {
	check(cr *checkRequest) (*admissionv1.AdmissionResponse, *httpErr)
	setNext(checker)
}

type checkRequest struct {
	newObj          *contourv1.HTTPProxy
	oldObj          *contourv1.HTTPProxy
	dryRun          *bool
	cache           *cache.Cache
	newIngressClass *ingressClass
	oldIngressClass *ingressClass
}

type ingressClass struct {
	name  string
	valid bool
}

//nolint:varnamelen
func validateV1(ar admissionv1.AdmissionReview, cache *cache.Cache) (*admissionv1.AdmissionResponse, *httpErr) {
	contourv1HttpproxyResource := metav1.GroupVersionResource{Group: "projectcontour.io", Version: "v1", Resource: "httpproxies"}

	if ar.Request.Resource != contourv1HttpproxyResource {
		return nil, &httpErr{code: http.StatusBadRequest,
			message: fmt.Sprintf("requested resource must be %s", contourv1HttpproxyResource)}
	}

	httpproxy := &contourv1.HTTPProxy{}
	httpproxyOld := &contourv1.HTTPProxy{}

	if _, _, err := deserializer.Decode(ar.Request.Object.Raw, nil, httpproxy); err != nil {
		return nil, &httpErr{code: http.StatusInternalServerError,
			message: fmt.Sprintf("requested resource could not be deserialized: %s", err.Error())}
	}

	if _, _, err := deserializer.Decode(ar.Request.OldObject.Raw, nil, httpproxyOld); err != nil {
		return nil, &httpErr{code: http.StatusInternalServerError,
			message: fmt.Sprintf("requested resource could not be deserialized: %s", err.Error())}
	}

	cr := &checkRequest{
		newObj: httpproxy,
		oldObj: httpproxyOld,
		dryRun: ar.Request.DryRun,
		cache:  cache,
	}
	warningValidatorRlsRules := rlsValidator{}

	switch ar.Request.Operation {
	case admissionv1.Create:
		cicnoc := checkIngressClassNameOnCreate{}
		cfoc := checkFqdnOnCreate{}

		cicnoc.setNext(&cfoc)

		response, err := cicnoc.check(cr)
		//warning rules
		response, err = validateWarningRules(response, err, cr, &warningValidatorRlsRules)

		return response, err

	case admissionv1.Update:
		cicnou := checkIngressClassNameOnUpdate{}
		cfou := checkFqdnOnUpdate{}

		cicnou.setNext(&cfou)

		response, err := cicnou.check(cr)
		//warning rules
		response, err = validateWarningRules(response, err, cr, &warningValidatorRlsRules)

		return response, err

	case admissionv1.Delete:
		cicnod := checkIngressClassNameOnDelete{}
		cfod := checkFqdnOnDelete{}

		cicnod.setNext(&cfod)

		response, err := cicnod.check(cr)

		return response, err
	}

	return nil, &httpErr{code: http.StatusBadRequest,
		message: "operation being performed on the requested resource must be one of CREATE, UPDATE or DELETE"}
}
