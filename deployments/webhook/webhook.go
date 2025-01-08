package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"

    admissionv1 "k8s.io/api/admission/v1"
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
    scheme = runtime.NewScheme()
    codecs = serializer.NewCodecFactory(scheme)
)

func init() {
    _ = admissionv1.AddToScheme(scheme)
}

func mutatePods(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
    podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
    if ar.Request.Resource != podResource {
        err := fmt.Errorf("expect resource to be %s", podResource)
        return &admissionv1.AdmissionResponse{
            Result: &metav1.Status{
                Message: err.Error(),
            },
        }
    }

    raw := ar.Request.Object.Raw
    pod := v1.Pod{}
    deserializer := codecs.UniversalDeserializer()
    if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
        return &admissionv1.AdmissionResponse{
            Result: &metav1.Status{
                Message: err.Error(),
            },
        }
    }

    // Check if the RuntimeClassName is set
    if pod.Spec.RuntimeClassName != nil {
        runtimeClassName := *pod.Spec.RuntimeClassName

        // Inject the RuntimeClassName as an environment variable into all containers
        for i := range pod.Spec.Containers {
            pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
                Name:  "RUNTIME_CLASS",
                Value: runtimeClassName,
            })
        }
    }

    // Create JSON patch response
    patchBytes, err := json.Marshal(pod.Spec.Containers)
    if err != nil {
        return &admissionv1.AdmissionResponse{
            Result: &metav1.Status{
                Message: err.Error(),
            },
        }
    }

    patchType := admissionv1.PatchTypeJSONPatch
    return &admissionv1.AdmissionResponse{
        Allowed:   true,
        Patch:     []byte(fmt.Sprintf("[{\"op\":\"replace\",\"path\":\"/spec/containers\",\"value\":%s}]", string(patchBytes))),
        PatchType: &patchType,
    }
}

func serve(w http.ResponseWriter, r *http.Request) {
    var body []byte
    if r.Body != nil {
        if data, err := ioutil.ReadAll(r.Body); err == nil {
            body = data
        }
    }

    if len(body) == 0 {
        http.Error(w, "empty body", http.StatusBadRequest)
        return
    }

    contentType := r.Header.Get("Content-Type")
    if contentType != "application/json" {
        http.Error(w, "invalid content type, expect `application/json`", http.StatusUnsupportedMediaType)
        return
    }

    ar := admissionv1.AdmissionReview{}
    if _, _, err := codecs.UniversalDeserializer().Decode(body, nil, &ar); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    admissionResponse := mutatePods(&ar)

    admissionReview := admissionv1.AdmissionReview{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
	},
        Response: admissionResponse,
    }
    admissionReview.Response.UID = ar.Request.UID

    resp, err := json.Marshal(admissionReview)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.Write(resp)
}

func main() {
    http.HandleFunc("/mutate", serve)
    server := &http.Server{
        Addr: ":8080",
    }

    fmt.Println("Starting Webhook Server...")
    if err := server.ListenAndServeTLS("/etc/webhook/certs/tls.crt", "/etc/webhook/certs/tls.key"); err != nil {
        fmt.Printf("Failed to start server: %v\n", err)
    }
}
