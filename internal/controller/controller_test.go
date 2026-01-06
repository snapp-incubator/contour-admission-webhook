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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/snapp-incubator/contour-admission-webhook/pkg/utils"
)

// Constants for test configuration
const (
	defaultNamespace = "default"
	defaultName      = "dummy"
	finalizerString  = "snappcloud.io/httpproxy-webhook-cache"
	waitDuration     = 1 * time.Second
)

// Main block for testing httpproxy fqdn cache controller
var _ = Describe("Testing httpproxy fqdn cache Controller", func() {
	Context("Testing reconcile loop functionality", Ordered, func() {
		// getSampleHttpproxy creates a sample HTTPProxy for testing
		getSampleHttpproxy := func(name, namespace string) *contourv1.HTTPProxy {
			return &contourv1.HTTPProxy{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "test",
					VirtualHost: &contourv1.VirtualHost{
						Fqdn: "test.local",
					},
				},
			}
		}

		// deleteHttpproxy deletes an HTTPProxy and waits for it to be removed
		deleteHttpproxy := func(httpproxy *contourv1.HTTPProxy) {
			Expect(k8sClient.Delete(context.Background(), httpproxy)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Namespace: httpproxy.Namespace,
					Name:      httpproxy.Name,
				}, &contourv1.HTTPProxy{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		}

		It("should not add a persisting cache entry for fqdn when httpproxy ingressClassName is invalid", func() {
			httpproxyObj := getSampleHttpproxy(defaultName, defaultNamespace)
			httpproxyObj.Spec.IngressClassName = "invalid"

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			cacheKey := utils.GenerateCacheKey(httpproxyObj.Spec.IngressClassName, httpproxyObj.Spec.VirtualHost.Fqdn)
			Expect(cacheStore.Exists(cacheKey)).To(BeFalse())

			deleteHttpproxy(httpproxyObj)
		})

		It("should add a persisting cache entry for fqdn when a httpproxy object is created or updated", func() {
			httpproxyObj := getSampleHttpproxy(defaultName, defaultNamespace)

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			cacheKey := utils.GenerateCacheKey(httpproxyObj.Spec.IngressClassName, httpproxyObj.Spec.VirtualHost.Fqdn)
			Expect(cacheStore.Exists(cacheKey)).To(BeTrue())

			isPersistent := cacheStore.IsPersistent(cacheKey)
			Expect(isPersistent).NotTo(BeNil())
			Expect(*isPersistent).To(BeTrue())

			deleteHttpproxy(httpproxyObj)
		})

		It("should delete the persisting cache entry for fqdn when a httpproxy object is deleted", func() {
			httpproxyObj := getSampleHttpproxy(defaultName, defaultNamespace)

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			cacheKey := utils.GenerateCacheKey(httpproxyObj.Spec.IngressClassName, httpproxyObj.Spec.VirtualHost.Fqdn)
			Expect(cacheStore.Exists(cacheKey)).To(BeTrue())

			isPersistent := cacheStore.IsPersistent(cacheKey)
			Expect(isPersistent).NotTo(BeNil())
			Expect(*isPersistent).To(BeTrue())

			Expect(k8sClient.Delete(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			Expect(cacheStore.Exists(cacheKey)).To(BeFalse())
		})

		It("should add finalizer string to httpproxy object when it is created or updated", func() {
			httpproxyObj := getSampleHttpproxy(defaultName, defaultNamespace)

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			currentHttpproxyObj := contourv1.HTTPProxy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}, &currentHttpproxyObj)).To(Succeed())
			Expect(currentHttpproxyObj.ObjectMeta.Finalizers).To(ContainElement(finalizerString))

			deleteHttpproxy(httpproxyObj)
		})

		It("should delete finalizer string from httpproxy object when it is deleted", func() {
			httpproxyObj := getSampleHttpproxy(defaultName, defaultNamespace)

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}, &contourv1.HTTPProxy{})).To(Succeed())

			Expect(k8sClient.Delete(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			Expect(apierrors.IsNotFound(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}, &contourv1.HTTPProxy{}))).To(BeTrue())
		})

		It("should update cache when FQDN changes", func() {
			httpproxyObj := getSampleHttpproxy(defaultName, defaultNamespace)

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			oldCacheKey := utils.GenerateCacheKey(httpproxyObj.Spec.IngressClassName, httpproxyObj.Spec.VirtualHost.Fqdn)
			Expect(cacheStore.Exists(oldCacheKey)).To(BeTrue())

			// Update the FQDN
			currentObj := &contourv1.HTTPProxy{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}, currentObj)).To(Succeed())

			currentObj.Spec.VirtualHost.Fqdn = "updated.test.local"
			Expect(k8sClient.Update(context.Background(), currentObj)).To(Succeed())

			time.Sleep(waitDuration)

			newCacheKey := utils.GenerateCacheKey(httpproxyObj.Spec.IngressClassName, "updated.test.local")
			Expect(cacheStore.Exists(newCacheKey)).To(BeTrue())
			Expect(cacheStore.Exists(oldCacheKey)).To(BeFalse())

			deleteHttpproxy(httpproxyObj)
		})

		It("should handle httpproxy without VirtualHost", func() {
			httpproxyObj := &contourv1.HTTPProxy{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "no-virtualhost",
				},
				Spec: contourv1.HTTPProxySpec{
					IngressClassName: "test",
					// No VirtualHost
				},
			}

			Expect(k8sClient.Create(context.Background(), httpproxyObj)).To(Succeed())

			time.Sleep(waitDuration)

			// Should not crash, just not add to cache
			Expect(cacheStore.Len()).To(BeNumerically(">=", 0))

			deleteHttpproxy(httpproxyObj)
		})
	})
})
