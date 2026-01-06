package webhook

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	jsoniter "github.com/json-iterator/go"
	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/snapp-incubator/contour-admission-webhook/internal/cache"
	"github.com/snapp-incubator/contour-admission-webhook/internal/config"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	apiserver "k8s.io/apiserver/pkg/server"
	apiserver_options "k8s.io/apiserver/pkg/server/options"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	contentTypeJSON     = "application/json"
	maxRequestBodyBytes = 1 << 20 // 1 MB
)

var (
	scheme       = runtime.NewScheme()
	codecFactory = serializer.NewCodecFactory(scheme)
	deserializer = codecFactory.UniversalDeserializer()

	json = jsoniter.ConfigCompatibleWithStandardLibrary

	entryTTL time.Duration

	logger = ctrl.Log.WithName("webhook")
)

func init() {
	utilruntime.Must(admissionv1.AddToScheme(scheme))
	utilruntime.Must(contourv1.AddToScheme(scheme))
}

type serverOptions struct {
	secureServingOptions apiserver_options.SecureServingOptions
}

func newServerOptions(port int, cert, key string) *serverOptions {
	return &serverOptions{
		secureServingOptions: apiserver_options.SecureServingOptions{
			BindAddress: net.IPv4zero,
			BindPort:    port,
			ServerCert: apiserver_options.GeneratableKeyCert{
				CertKey: apiserver_options.CertKey{
					CertFile: cert,
					KeyFile:  key,
				},
			},
		},
	}
}

type serverConfig struct {
	secureServingInfo *apiserver.SecureServingInfo
}

func (so *serverOptions) newServerConfig() *serverConfig {
	sc := &serverConfig{}

	if err := so.secureServingOptions.ApplyTo(&sc.secureServingInfo); err != nil {
		panic(fmt.Errorf("failed to apply secure serving options: %w", err))
	}

	return sc
}

// admitFunc is the function signature for admission handlers.
type admitFunc func(admissionv1.AdmissionReview, *cache.Cache) (*admissionv1.AdmissionResponse, *httpErr)

// admissionHandler handles admission webhook requests.
type admissionHandler struct {
	cache   *cache.Cache
	handler admitFunc
}

var _ http.Handler = (*admissionHandler)(nil)

// httpErr represents an HTTP error response.
type httpErr struct {
	code    int
	message string
}

func (e *httpErr) Error() string {
	return fmt.Sprintf("code=%d, message=%s", e.code, e.message)
}

func newHTTPError(code int, format string, args ...interface{}) *httpErr {
	return &httpErr{
		code:    code,
		message: fmt.Sprintf(format, args...),
	}
}

// ServeHTTP handles the HTTP portion of a request prior to handing to an admit function.
func (ah *admissionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read request body: %s", err.Error()), http.StatusBadRequest)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != contentTypeJSON {
		http.Error(w, fmt.Sprintf("invalid Content-Type %q, expected %q", contentType, contentTypeJSON), http.StatusUnsupportedMediaType)
		return
	}

	obj, gvk, err := deserializer.Decode(body, nil, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request body: %s", err.Error()), http.StatusBadRequest)
		return
	}

	admissionReviewRequest, ok := obj.(*admissionv1.AdmissionReview)
	if !ok {
		http.Error(w, fmt.Sprintf("expected AdmissionReview but got %T", obj), http.StatusBadRequest)
		return
	}

	if admissionReviewRequest.Request == nil {
		http.Error(w, "admission review request is nil", http.StatusBadRequest)
		return
	}

	// Call the handler
	admitResponse, handlerErr := ah.handler(*admissionReviewRequest, ah.cache)
	if handlerErr != nil {
		http.Error(w, handlerErr.message, handlerErr.code)
		return
	}

	if admitResponse == nil {
		http.Error(w, "handler returned nil response", http.StatusInternalServerError)
		return
	}

	// Build response
	admissionReviewResponse := &admissionv1.AdmissionReview{}
	admissionReviewResponse.SetGroupVersionKind(*gvk)
	admissionReviewResponse.Response = admitResponse
	admissionReviewResponse.Response.UID = admissionReviewRequest.Request.UID

	jsonData, err := json.Marshal(admissionReviewResponse)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusOK)

	if _, err = w.Write(jsonData); err != nil {
		logger.Error(err, "failed to write response")
	}
}

// readinessHandler handles readiness probe requests.
func readinessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte("ok")); err != nil {
		logger.Error(err, "failed to write readiness response")
	}
}

// livenessHandler handles liveness probe requests.
func livenessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte("ok")); err != nil {
		logger.Error(err, "failed to write liveness response")
	}
}

// Setup configures and starts the webhook server.
// Returns channels that are closed when the server stops.
func Setup(c *cache.Cache) (<-chan struct{}, <-chan struct{}) {
	cfg := config.GetConfig()

	// Set the global TTL for cache entries created by the webhook
	entryTTL = time.Duration(cfg.Cache.EntryTTLSecond) * time.Second

	serverOptions := newServerOptions(cfg.Webhook.Port, cfg.Webhook.TLSCertFile, cfg.Webhook.TLSKeyFile)
	serverConfig := serverOptions.newServerConfig()

	mux := http.NewServeMux()
	mux.Handle("/v1/validate", &admissionHandler{cache: c, handler: validateV1})
	mux.HandleFunc("/readyz", readinessHandler)
	mux.HandleFunc("/healthz", livenessHandler)

	stopCh := apiserver.SetupSignalHandler()

	stoppedCh, listenerStoppedCh, err := serverConfig.secureServingInfo.Serve(mux, 30*time.Second, stopCh)
	if err != nil {
		panic(fmt.Errorf("failed to start webhook server: %w", err))
	}

	logger.Info("webhook server started", "port", cfg.Webhook.Port)

	return stoppedCh, listenerStoppedCh
}

