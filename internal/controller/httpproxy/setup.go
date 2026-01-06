package controller

import (
	"context"
	"errors"
	"fmt"

	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/snapp-incubator/contour-admission-webhook/internal/cache"
	"github.com/snapp-incubator/contour-admission-webhook/pkg/utils"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ handler.EventHandler = (*customEventHandler)(nil)

func newCustomEventHandler(handlerFunc customEventHandlerFunc) handler.EventHandler {
	return &customEventHandler{
		handler: handlerFunc,
	}
}

func (h *customEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	for _, req := range h.handler(ctx, evt.Object, nil, createEvent) {
		q.Add(req)
	}
}

func (h *customEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	for _, req := range h.handler(ctx, evt.ObjectNew, evt.ObjectOld, updateEvent) {
		q.Add(req)
	}
}

func (h *customEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	for _, req := range h.handler(ctx, nil, evt.Object, deleteEvent) {
		q.Add(req)
	}
}

func (h *customEventHandler) Generic(_ context.Context, _ event.GenericEvent, _ workqueue.RateLimitingInterface) {
	// No implementation needed for generic events
}

// httpproxyEventHandler handles HTTPProxy events and updates the cache accordingly.
func (re *ReconcilerExtended) httpproxyEventHandler(ctx context.Context, objNew, objOld client.Object, et eventType) []ctrl.Request {
	logger := log.FromContext(ctx).WithName("httpproxy-event-handler").WithValues("event", et)

	var newProxy, oldProxy *contourv1.HTTPProxy

	if objNew != nil {
		var ok bool
		newProxy, ok = objNew.(*contourv1.HTTPProxy)
		if !ok {
			logger.Error(nil, "unexpected object type for new object", "type", fmt.Sprintf("%T", objNew))
			return nil
		}
	}

	if objOld != nil {
		var ok bool
		oldProxy, ok = objOld.(*contourv1.HTTPProxy)
		if !ok {
			logger.Error(nil, "unexpected object type for old object", "type", fmt.Sprintf("%T", objOld))
			return nil
		}
	}

	// Always queue for reconciliation
	var reqs []ctrl.Request

	switch et {
	case createEvent:
		reqs = re.handleCreate(logger, newProxy)
	case updateEvent:
		reqs = re.handleUpdate(logger, newProxy, oldProxy)
	case deleteEvent:
		reqs = re.handleDelete(logger, oldProxy)
	}

	return reqs
}

func (re *ReconcilerExtended) handleCreate(logger interface{ Info(string, ...interface{}) }, proxy *contourv1.HTTPProxy) []ctrl.Request {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: proxy.GetNamespace(),
			Name:      proxy.GetName(),
		},
	}

	if proxy.Spec.VirtualHost == nil {
		return []ctrl.Request{req}
	}

	ingressClassName := utils.GetIngressClassName(proxy)
	if !utils.ValidateIngressClassName(ingressClassName) {
		logger.Info("httpproxy has invalid ingressClassName, skipping cache update")
		return []ctrl.Request{req}
	}

	fqdn := proxy.Spec.VirtualHost.Fqdn
	cacheKey := utils.GenerateCacheKey(ingressClassName, fqdn)

	// Check if key already exists as persistent (would indicate duplicate)
	if isPersistent := re.cache.IsPersistent(cacheKey); isPersistent != nil && *isPersistent {
		err := errors.New("fqdn uniqueness violation")
		logger.Info("fqdn is already persisted in cache, possible duplicate", "fqdn", fqdn, "error", err)
		return []ctrl.Request{req}
	}

	// Set as persistent (never expires - managed by controller)
	re.cache.SetPersistent(cacheKey, types.NamespacedName{
		Namespace: proxy.GetNamespace(),
		Name:      proxy.GetName(),
	})

	return []ctrl.Request{req}
}

func (re *ReconcilerExtended) handleUpdate(logger interface{ Info(string, ...interface{}) }, newProxy, oldProxy *contourv1.HTTPProxy) []ctrl.Request {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: newProxy.GetNamespace(),
			Name:      newProxy.GetName(),
		},
	}

	newHasVH := newProxy.Spec.VirtualHost != nil
	oldHasVH := oldProxy.Spec.VirtualHost != nil

	// Neither has VirtualHost - nothing to do
	if !newHasVH && !oldHasVH {
		return []ctrl.Request{req}
	}

	oldIngressClassName := utils.GetIngressClassName(oldProxy)

	// VirtualHost removed - delete old cache entry
	if !newHasVH && oldHasVH {
		oldFqdn := oldProxy.Spec.VirtualHost.Fqdn
		cacheKey := utils.GenerateCacheKey(oldIngressClassName, oldFqdn)
		re.cache.Delete(cacheKey)
		return []ctrl.Request{req}
	}

	newIngressClassName := utils.GetIngressClassName(newProxy)

	// VirtualHost added - add new cache entry
	if newHasVH && !oldHasVH {
		newFqdn := newProxy.Spec.VirtualHost.Fqdn
		cacheKey := utils.GenerateCacheKey(newIngressClassName, newFqdn)
		re.cache.SetPersistent(cacheKey, types.NamespacedName{
			Namespace: newProxy.GetNamespace(),
			Name:      newProxy.GetName(),
		})
		return []ctrl.Request{req}
	}

	// Both have VirtualHost - check for changes
	newFqdn := newProxy.Spec.VirtualHost.Fqdn
	oldFqdn := oldProxy.Spec.VirtualHost.Fqdn

	if newFqdn != oldFqdn || newIngressClassName != oldIngressClassName {
		// Add new entry
		newCacheKey := utils.GenerateCacheKey(newIngressClassName, newFqdn)
		re.cache.SetPersistent(newCacheKey, types.NamespacedName{
			Namespace: newProxy.GetNamespace(),
			Name:      newProxy.GetName(),
		})

		// Delete old entry
		oldCacheKey := utils.GenerateCacheKey(oldIngressClassName, oldFqdn)
		re.cache.Delete(oldCacheKey)
	}

	return []ctrl.Request{req}
}

func (re *ReconcilerExtended) handleDelete(logger interface{ Info(string, ...interface{}) }, proxy *contourv1.HTTPProxy) []ctrl.Request {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: proxy.GetNamespace(),
			Name:      proxy.GetName(),
		},
	}

	if proxy.Spec.VirtualHost == nil {
		return []ctrl.Request{req}
	}

	ingressClassName := utils.GetIngressClassName(proxy)
	fqdn := proxy.Spec.VirtualHost.Fqdn
	cacheKey := utils.GenerateCacheKey(ingressClassName, fqdn)

	re.cache.Delete(cacheKey)

	return []ctrl.Request{req}
}

// NewReconcilerExtended creates a new ReconcilerExtended instance.
func NewReconcilerExtended(mgr manager.Manager, cache *cache.Cache) *ReconcilerExtended {
	return &ReconcilerExtended{
		cache:  cache,
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}
}

// SetupWithManager sets up the controller with the manager.
func (re *ReconcilerExtended) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Watches(&contourv1.HTTPProxy{}, newCustomEventHandler(re.httpproxyEventHandler)).
		Named("httpproxy").
		Complete(re)
}
