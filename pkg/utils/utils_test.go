/*
Copyright 2024.

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

package utils

import (
	"os"
	"path/filepath"
	"testing"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/snapp-incubator/contour-admission-webhook/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setupTestConfig(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
cache:
  cleanUpIntervalSecond: 30
  entryTtlSecond: 10
ingressClasses:
  - "public"
  - "private"
  - "test"
webhook:
  port: 8443
  tlsCertFile: "/etc/certs/tls.crt"
  tlsKeyFile: "/etc/certs/tls.key"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	config.ResetForTesting()
	ResetValidIngressClassesForTesting()

	err = config.InitializeConfig(configPath)
	require.NoError(t, err)
}

func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		name             string
		ingressClassName string
		fqdn             string
		expected         string
	}{
		{
			name:             "basic key",
			ingressClassName: "public",
			fqdn:             "example.com",
			expected:         "public/example.com",
		},
		{
			name:             "with subdomain",
			ingressClassName: "private",
			fqdn:             "app.example.com",
			expected:         "private/app.example.com",
		},
		{
			name:             "empty ingress class",
			ingressClassName: "",
			fqdn:             "example.com",
			expected:         "/example.com",
		},
		{
			name:             "empty fqdn",
			ingressClassName: "public",
			fqdn:             "",
			expected:         "public/",
		},
		{
			name:             "wildcard fqdn",
			ingressClassName: "public",
			fqdn:             "*.example.com",
			expected:         "public/*.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateCacheKey(tt.ingressClassName, tt.fqdn)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetIngressClassName(t *testing.T) {
	tests := []struct {
		name      string
		httpproxy *contourv1.HTTPProxy
		expected  string
	}{
		{
			name:      "nil httpproxy",
			httpproxy: nil,
			expected:  "",
		},
		{
			name: "from spec field",
			httpproxy: &contourv1.HTTPProxy{
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "public",
				},
			},
			expected: "public",
		},
		{
			name: "from annotation",
			httpproxy: &contourv1.HTTPProxy{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "private",
					},
				},
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "public",
				},
			},
			expected: "private", // annotation takes precedence
		},
		{
			name: "annotation empty uses spec",
			httpproxy: &contourv1.HTTPProxy{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "",
					},
				},
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "public",
				},
			},
			expected: "public",
		},
		{
			name: "no annotation uses spec",
			httpproxy: &contourv1.HTTPProxy{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"other-annotation": "value",
					},
				},
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "public",
				},
			},
			expected: "public",
		},
		{
			name: "nil annotations uses spec",
			httpproxy: &contourv1.HTTPProxy{
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "public",
				},
			},
			expected: "public",
		},
		{
			name: "empty spec and no annotation",
			httpproxy: &contourv1.HTTPProxy{
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "",
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetIngressClassName(tt.httpproxy)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateIngressClassName(t *testing.T) {
	setupTestConfig(t)

	tests := []struct {
		name             string
		ingressClassName string
		expected         bool
	}{
		{
			name:             "valid - public",
			ingressClassName: "public",
			expected:         true,
		},
		{
			name:             "valid - private",
			ingressClassName: "private",
			expected:         true,
		},
		{
			name:             "valid - test",
			ingressClassName: "test",
			expected:         true,
		},
		{
			name:             "invalid - unknown",
			ingressClassName: "unknown",
			expected:         false,
		},
		{
			name:             "invalid - empty",
			ingressClassName: "",
			expected:         false,
		},
		{
			name:             "invalid - case sensitive",
			ingressClassName: "Public",
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateIngressClassName(tt.ingressClassName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResetValidIngressClassesForTesting(t *testing.T) {
	setupTestConfig(t)

	// Validate that config is loaded
	assert.True(t, ValidateIngressClassName("public"))

	// Reset
	ResetValidIngressClassesForTesting()

	// After reset, the cache is cleared, but the config is still there
	// so the next call will reload
	assert.True(t, ValidateIngressClassName("public"))
}

// Benchmarks

func BenchmarkGenerateCacheKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateCacheKey("public", "example.com")
	}
}

func BenchmarkGetIngressClassName_FromSpec(b *testing.B) {
	proxy := &contourv1.HTTPProxy{
		Spec: contourv1.HTTPProxySpec{
			IngressClassName: "public",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetIngressClassName(proxy)
	}
}

func BenchmarkGetIngressClassName_FromAnnotation(b *testing.B) {
	proxy := &contourv1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "private",
			},
		},
		Spec: contourv1.HTTPProxySpec{
			IngressClassName: "public",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetIngressClassName(proxy)
	}
}

