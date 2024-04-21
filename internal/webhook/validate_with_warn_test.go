package webhook

import (
	"io"
	"os"
	"strings"
	"testing"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// getHTTPProxyFromYAML reads a YAML file containing an HTTPProxy object
// and returns the parsed HTTPProxy object.
//
//nolint:all
func getHTTPProxyFromYAML(path string) (*contourv1.HTTPProxy, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	httpProxyByte, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	httpProxyObject := &contourv1.HTTPProxy{}
	err = yaml.Unmarshal(httpProxyByte, httpProxyObject)
	return httpProxyObject, err
}

// Test_validateWarningRules is a unit test to validate the warning rules function.
func Test_validateWarningRules(t *testing.T) {
	// Read HTTPProxy object from YAML file.
	httpProxyObj, err := getHTTPProxyFromYAML("./testdata/httpProxy_rls.yaml")
	if err != nil {
		t.Error(err)
	}

	// Define test arguments.
	request := struct {
		givenResponse *admissionv1.AdmissionResponse
		request       *checkRequest
		checkers      []checker
	}{
		givenResponse: &admissionv1.AdmissionResponse{Allowed: true},
		request:       &checkRequest{newObj: httpProxyObj},
		checkers:      []checker{&rlsValidator{}},
	}

	// Test: An HTTPProxy with valid RLS configuration should not have warnings.
	acceptedResponse, _ := validateWarningRules(request.givenResponse, nil, request.request, request.checkers...)
	if !acceptedResponse.Allowed {
		t.Error("response for HTTPProxy with valid configuration should be allowed")
	}
	if len(acceptedResponse.Warnings) != 0 {
		t.Errorf("response for HTTPProxy with valid configuration should not have any warning messages: %v", acceptedResponse.Warnings)
	}

	// Test: A request with incorrect configuration should trigger warnings.
	request.request.newObj.Spec.Routes[0].RateLimitPolicy.Global.Descriptors[0].Entries[0].GenericKey.Key = "wrong.name.xx"
	acceptedResponseWithWarn, _ := validateWarningRules(request.givenResponse, nil, request.request, request.checkers...)
	if len(acceptedResponseWithWarn.Warnings) == 0 {
		t.Error("for request with incorrect configuration, warning should be sent")
	}
}

func Test_acceptWithWarning(t *testing.T) {
	type args struct {
		message string
	}
	tests := []struct {
		name string
		args args
		want *admissionv1.AdmissionResponse
	}{
		{
			name: "msg with value less than 120 char",
			args: args{message: "test"},
			want: &admissionv1.AdmissionResponse{Allowed: true, Warnings: []string{rlsPrefixMsg + "test"}},
		},
		{
			name: "msg with value more than 120 should truncate to 120",
			args: args{message: strings.Repeat("a", 120)},
			want: &admissionv1.AdmissionResponse{Allowed: true, Warnings: []string{rlsPrefixMsg + strings.Repeat("a", 120-len("Rate Limit Config Error: "))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := acceptWithWarning(rlsPrefixMsg, tt.args.message)
			if tt.want.Warnings[0] != got.Warnings[0] {
				t.Errorf("acceptWithWarning() got = %v, want %v", got, tt.want)
			}
		})
	}
}
