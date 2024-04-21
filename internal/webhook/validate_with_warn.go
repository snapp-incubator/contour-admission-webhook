package webhook

import (
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func validateWarningRules(response *admissionv1.AdmissionResponse, err *httpErr, request *checkRequest, checkers ...checker) (*admissionv1.AdmissionResponse, *httpErr) {
	//If the request is rejected during rule checking, then do not process the warning rules.
	if !response.Allowed {
		return response, err
	}

	msg := make([]string, 0)
	// Combine all warining msg together
	for _, c := range checkers {
		resp, _ := c.check(request)
		if len(resp.Warnings) > 0 {
			msg = append(msg, resp.Warnings...)
		}
	}

	if wariningResponseCount := len(msg); wariningResponseCount == 0 {
		//There isn't any warning
		return &admissionv1.AdmissionResponse{Allowed: true}, nil
	}
	//return all warnings
	return &admissionv1.AdmissionResponse{Allowed: true, Warnings: msg, Result: &metav1.Status{
		Code:    http.StatusAccepted,
		Message: fmt.Sprint(msg),
	}}, nil
}

func acceptWithWarning(prefix, message string) (*admissionv1.AdmissionResponse, *httpErr) {
	message = fmt.Sprint(prefix, message)

	messageMaxLenth := 120
	if lenMsg := len(message); lenMsg < 120 {
		messageMaxLenth = lenMsg
	}

	return &admissionv1.AdmissionResponse{Allowed: true, Warnings: []string{message[:messageMaxLenth]}, Result: &metav1.Status{
		Code:    http.StatusAccepted,
		Message: message,
	}}, nil
}
