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
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/snapp-incubator/contour-admission-webhook/internal/cache"
	"github.com/snapp-incubator/contour-admission-webhook/internal/config"
	controller "github.com/snapp-incubator/contour-admission-webhook/internal/controller/httpproxy"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cacheStore *cache.Cache
	k8sRestCfg *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")

	if err := config.InitializeConfig("../../hack/config.yaml"); err != nil {
		Expect(err).ToNot(HaveOccurred())
	}

	config := config.GetConfig()

	cacheStore = cache.NewCache(time.Duration(config.Cache.CleanUpIntervalSecond) * time.Second)

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "hack", "crd", "external")},
		ErrorIfCRDPathMissing: true,
	}

	var err error

	// cfg is defined in this file globally.
	k8sRestCfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sRestCfg).NotTo(BeNil())

	err = contourv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(k8sRestCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(k8sRestCfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sManager).NotTo(BeNil())

	reconcilerExtended := controller.NewReconcilerExtended(k8sManager, cacheStore)

	err = reconcilerExtended.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := (func() (err error) {
		// Need to sleep if the first stop fails due to a bug:
		// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
		sleepTime := 1 * time.Millisecond
		for i := 0; i < 12; i++ { // Exponentially sleep up to ~4s
			if err = testEnv.Stop(); err == nil {
				return
			}
			sleepTime *= 2
			time.Sleep(sleepTime)
		}
		return
	})()
	Expect(err).NotTo(HaveOccurred())
})
