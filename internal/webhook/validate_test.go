package webhook

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snapp-incubator/contour-admission-webhook/internal/cache"
	"github.com/snapp-incubator/contour-admission-webhook/internal/config"
	"github.com/snapp-incubator/contour-admission-webhook/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestValidate(t *testing.T) {
	// Reset config state for testing
	config.ResetForTesting()
	utils.ResetValidIngressClassesForTesting()

	if err := config.InitializeConfig("../../hack/config.test.yaml"); err != nil {
		require.NoError(t, err, "error reading the config file")
	}

	cfg := config.GetConfig()

	cacheCleanUpInterval := time.Duration(cfg.Cache.CleanUpIntervalSecond) * time.Second
	cacheDuration := time.Duration(cfg.Cache.EntryTTLSecond) * time.Second
	validIngressClassNames := cfg.IngressClasses
	invalidIngressClassName := "invalid"
	allIngressClassNames := append(validIngressClassNames, invalidIngressClassName)

	// Set the global entryTTL for the webhook tests
	entryTTL = cacheDuration

	t.Run("Should clean up expired keys from cache", func(t *testing.T) {
		testCache := cache.NewCache(1 * time.Second)
		defer testCache.Stop()

		testCache.SetWithTTL("test/key",
			types.NamespacedName{Namespace: "test", Name: "test"},
			1*time.Second,
		)

		assert.True(t, testCache.Exists("test/key"))
		time.Sleep(3 * time.Second)
		assert.False(t, testCache.Exists("test/key"))
	})

	t.Run("Should not clean up keys without expiration time", func(t *testing.T) {
		testCache := cache.NewCache(1 * time.Second)
		defer testCache.Stop()

		testCache.SetPersistent("test/key",
			types.NamespacedName{Namespace: "test", Name: "test"},
		)

		assert.True(t, testCache.Exists("test/key"))
		time.Sleep(3 * time.Second)
		assert.True(t, testCache.Exists("test/key"))
	})

	t.Run("Should return error indicating invalid Content-Type header", func(t *testing.T) {
		testCache := cache.NewCache(cacheCleanUpInterval)
		defer testCache.Stop()

		ah := &admissionHandler{
			cache:   testCache,
			handler: validateV1,
		}

		r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte{}))
		r.Header.Set("Content-Type", "text/plain")

		w := httptest.NewRecorder()

		ah.ServeHTTP(w, r)

		assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	})

	t.Run("Should deny the admission request - CREATE operation with invalid ingressClassName", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "abb483ba-8193-4bef-9a39-245646e30506",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "CREATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": null,
					"dryRun": false
				}
			}
		`

		ingressClassName := invalidIngressClassName
		fqdn := "test.local"

		localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, fqdn)

		testCache := cache.NewCache(cacheCleanUpInterval)
		defer testCache.Stop()

		ah := &admissionHandler{
			cache:   testCache,
			handler: validateV1,
		}

		admissionReviewResponse := &admissionv1.AdmissionReview{}

		r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
		r.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()

		ah.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
		assert.False(t, admissionReviewResponse.Response.Allowed)
	})

	t.Run("Should allow the admission request and add a cache entry for the requested FQDN - CREATE operation with valid ingressClassName", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "abb483ba-8193-4bef-9a39-245646e30506",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "CREATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": null,
					"dryRun": false
				}
			}
		`

		fqdn := "test.local"

		for _, ingressClassName := range validIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, fqdn)

			cacheKey := utils.GenerateCacheKey(ingressClassName, fqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			isFqdnAdded := testCache.Exists(cacheKey)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, isFqdnAdded)
			assert.True(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should allow the admission request and not alter the cache for dry-run requests - CREATE operation", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "abb483ba-8193-4bef-9a39-245646e30506",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "CREATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": null,
					"dryRun": true
				}
			}
		`

		fqdn := "test.local"

		for _, ingressClassName := range validIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, fqdn)

			cacheKey := utils.GenerateCacheKey(ingressClassName, fqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			isFqdnAdded := testCache.Exists(cacheKey)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.False(t, isFqdnAdded) // Dry-run should not add to cache
			assert.True(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should deny the admission request because of the acquired FQDN - CREATE operation", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "abb483ba-8193-4bef-9a39-245646e30506",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "CREATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": null,
					"dryRun": false
				}
			}
		`

		fqdn := "test.local"

		for _, ingressClassName := range validIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, fqdn)

			cacheKey := utils.GenerateCacheKey(ingressClassName, fqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)
			testCache.SetWithTTL(cacheKey,
				types.NamespacedName{Namespace: "other", Name: "other"},
				cacheDuration,
			)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.False(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should allow UPDATE when FQDN changes to an available one", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "529df94e-15df-47db-9959-07dab4a9effb",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "UPDATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"dryRun": false
				}
			}
		`

		newFqdn := "new.test.local"
		oldFqdn := "old.test.local"

		for _, ingressClassName := range validIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, newFqdn, ingressClassName, oldFqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			newCacheKey := utils.GenerateCacheKey(ingressClassName, newFqdn)
			isFqdnAdded := testCache.Exists(newCacheKey)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, isFqdnAdded)
			assert.True(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should deny UPDATE when new FQDN is already acquired", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "529df94e-15df-47db-9959-07dab4a9effb",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "UPDATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"dryRun": false
				}
			}
		`

		newFqdn := "new.test.local"
		oldFqdn := "old.test.local"

		for _, ingressClassName := range validIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, newFqdn, ingressClassName, oldFqdn)

			newCacheKey := utils.GenerateCacheKey(ingressClassName, newFqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)
			testCache.SetWithTTL(newCacheKey,
				types.NamespacedName{Namespace: "other", Name: "other"},
				cacheDuration,
			)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.False(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should allow UPDATE when neither FQDN nor ingressClass changes", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "529df94e-15df-47db-9959-07dab4a9effb",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "UPDATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"oldObject": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"dryRun": false
				}
			}
		`

		sameFqdn := "same.test.local"

		for _, ingressClassName := range validIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, sameFqdn, ingressClassName, sameFqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should allow DELETE operation", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "833f7f5c-5df8-4942-8e2b-11d4c20f81d5",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "DELETE",
					"userInfo": {"username": "test"},
					"object": null,
					"oldObject": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "%s",
							"virtualhost": {"fqdn": "%s"}
						}
					},
					"dryRun": false
				}
			}
		`

		fqdn := "test.local"

		for _, ingressClassName := range allIngressClassNames {
			localAdmissionRequestJSON := fmt.Sprintf(admissionRequestJSON, ingressClassName, fqdn)

			testCache := cache.NewCache(cacheCleanUpInterval)

			ah := &admissionHandler{
				cache:   testCache,
				handler: validateV1,
			}

			admissionReviewResponse := &admissionv1.AdmissionReview{}

			r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(localAdmissionRequestJSON)))
			r.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			ah.ServeHTTP(w, r)

			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, admissionReviewResponse.Response.Allowed)

			testCache.Stop()
		}
	})

	t.Run("Should handle HTTPProxy without VirtualHost in CREATE", func(t *testing.T) {
		admissionRequestJSON := `
			{
				"kind": "AdmissionReview",
				"apiVersion": "admission.k8s.io/v1",
				"request": {
					"uid": "abb483ba-8193-4bef-9a39-245646e30506",
					"kind": {"group": "projectcontour.io", "version": "v1", "kind": "HTTPProxy"},
					"resource": {"group": "projectcontour.io", "version": "v1", "resource": "httpproxies"},
					"name": "test",
					"namespace": "test",
					"operation": "CREATE",
					"userInfo": {"username": "test"},
					"object": {
						"apiVersion": "projectcontour.io/v1",
						"kind": "HTTPProxy",
						"metadata": {"name": "test", "namespace": "test"},
						"spec": {
							"ingressClassName": "test"
						}
					},
					"oldObject": null,
					"dryRun": false
				}
			}
		`

		testCache := cache.NewCache(cacheCleanUpInterval)
		defer testCache.Stop()

		ah := &admissionHandler{
			cache:   testCache,
			handler: validateV1,
		}

		admissionReviewResponse := &admissionv1.AdmissionReview{}

		r := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader([]byte(admissionRequestJSON)))
		r.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()

		ah.ServeHTTP(w, r)

		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), admissionReviewResponse))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, admissionReviewResponse.Response.Allowed)
		assert.Equal(t, 0, testCache.Len()) // No cache entries for proxy without VirtualHost
	})
}
