package webhook

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/labstack/echo/v4"
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

var (
	scheme       = runtime.NewScheme()
	codecFactory = serializer.NewCodecFactory(scheme)
	deserializer = codecFactory.UniversalDeserializer()

	json = jsoniter.ConfigCompatibleWithStandardLibrary

	entryTtlSecond        int
	idleConnectionTimeout string

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
	//nolint:varnamelen
	so := &serverOptions{
		secureServingOptions: apiserver_options.SecureServingOptions{
			BindAddress: net.IP{0, 0, 0, 0},
			BindPort:    port,
			ServerCert: apiserver_options.GeneratableKeyCert{
				CertKey: apiserver_options.CertKey{
					CertFile: cert,
					KeyFile:  key,
				},
			},
		},
	}

	return so
}

type serverConfig struct {
	secureServingInfo *apiserver.SecureServingInfo
}

func (so *serverOptions) newServerConfig() *serverConfig {
	//nolint:varnamelen
	sc := &serverConfig{}

	if err := so.secureServingOptions.ApplyTo(&sc.secureServingInfo); err != nil {
		panic(err)
	}

	return sc
}

type admitV1Func func(admissionv1.AdmissionReview, *cache.Cache) (*admissionv1.AdmissionResponse, *httpErr)

type admissionHandler struct {
	cache   *cache.Cache
	handler admitV1Func
}

var _ http.Handler = &admissionHandler{}

type httpErr struct {
	code    int
	message interface{}
}

func (he httpErr) Error() string {
	return fmt.Sprintf("code=%d, message=%v", he.code, he.message)
}

// ServeHTTP handles the http portion of a request prior to handing to an admit function.
//
//nolint:varnamelen
func (ah *admissionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte

	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not read request body: %s", err.Error()), http.StatusInternalServerError)

			return
		}

		body = data
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != echo.MIMEApplicationJSON {
		http.Error(w, fmt.Sprintf("content-type header is %s, must be application/json", contentType), http.StatusBadRequest)

		return
	}

	var responseObj runtime.Object

	obj, gvk, err := deserializer.Decode(body, nil, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("http request body could not be decoded: %s", err.Error()), http.StatusBadRequest)

		return
	}

	admissionReviewRequest, ok := obj.(*admissionv1.AdmissionReview)
	if !ok {
		http.Error(w, fmt.Sprintf("expected v1.AdmissionReview object but got: %T object", obj), http.StatusBadRequest)

		return
	}

	// Can not use the already declared err interface
	// Impossible comparison of interface value with untyped nil
	// https://staticcheck.dev/docs/checks#SA4023
	admitResponse, admitHttpError := ah.handler(*admissionReviewRequest, ah.cache)
	if admitHttpError != nil {
		http.Error(w, admitHttpError.message.(string), admitHttpError.code)

		return
	}

	admissionReviewResponse := &admissionv1.AdmissionReview{}
	admissionReviewResponse.SetGroupVersionKind(*gvk)
	admissionReviewResponse.Response = admitResponse
	admissionReviewResponse.Response.UID = admissionReviewRequest.Request.UID
	responseObj = admissionReviewResponse

	jsonData, err := json.Marshal(responseObj)
	if err != nil {
		http.Error(w, fmt.Sprintf("error encoding response object to json: %s", err.Error()), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(jsonData)
	if err != nil {
		logger.Error(err, "error writing the data to the connection as part of an http reply")
	}
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	_, err := w.Write([]byte("ok"))
	if err != nil {
		logger.Error(err, "error writing the data to the connection as part of an http reply")
	}
}

type Webhook struct {
	cache          *cache.Cache
	shouldMutate   bool
	shouldValidate bool
}

func NewWebhook() *Webhook {
	return &Webhook{}
}

func (wh *Webhook) AddCache(cache *cache.Cache) *Webhook {
	wh.cache = cache

	return wh
}

func (wh *Webhook) ShouldMutate() *Webhook {
	wh.shouldMutate = true

	return wh
}

func (wh *Webhook) ShouldValidate() *Webhook {
	wh.shouldValidate = true

	return wh
}

func (wh *Webhook) Setup(stopCh <-chan struct{}) (<-chan struct{}, <-chan struct{}) {
	// Populate the global variable once to prevent further resource allocations per validation request
	cfg := config.GetConfig()
	entryTtlSecond = cfg.Cache.EntryTtlSecond
	idleConnectionTimeout = cfg.IdleConnectionTimeout

	serverOptions := newServerOptions(cfg.Webhook.Port, cfg.Webhook.TLSCertFile, cfg.Webhook.TLSKeyFile)

	serverConfig := serverOptions.newServerConfig()

	mux := http.NewServeMux()

	if wh.shouldValidate {
		if wh.cache == nil {
			panic(errors.New("cache is required for webhook validate endpoint"))
		}

		mux.Handle("/v1/validate", &admissionHandler{cache: wh.cache, handler: validateV1})
	}

	if wh.shouldMutate {
		mux.Handle("/v1/mutate", &admissionHandler{cache: nil, handler: mutateV1})
	}

	mux.Handle("/readyz", http.HandlerFunc(readinessHandler))

	stoppedCh, listenerStoppedCh, err := serverConfig.secureServingInfo.Serve(mux, 30*time.Second, stopCh)
	if err != nil {
		panic(err)
	}

	return stoppedCh, listenerStoppedCh
}
