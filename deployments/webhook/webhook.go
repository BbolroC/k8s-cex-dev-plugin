package main

import (
    "encoding/json"
    "fmt"
    "strings"
    "io/ioutil"
    "net/http"

    admissionv1 "k8s.io/api/admission/v1"
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "github.com/evanphx/json-patch"
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

func escapeJSONPointer(s string) string {
    // Replace `/` with `~1` to escape JSON Pointer paths.
    s = strings.ReplaceAll(s, "/", "~1")
    return s
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

    // Create patches for both limits and requests
    var patches []map[string]interface{}
    runtimeClassName := pod.Spec.RuntimeClassName
    allowedRuntimeClasses := map[string]bool {
        "kata": true,
        "kata-qemu": true,
        "kata-qemu-se": true,
    }
    for i, container := range pod.Spec.Containers {
        // Handle limits
        if limits := container.Resources.Limits; limits != nil {
            newLimits := make(map[string]interface{})
            for key, value := range limits {
                keyStr := string(key)
                if runtimeClassName != nil &&
                    allowedRuntimeClasses[*runtimeClassName] &&
		    strings.HasPrefix(keyStr, "cex.s390.ibm.com/") {

                    newKey := strings.Replace(keyStr, "cex.s390.ibm.com", "mdev.s390.ibm.com", 1)
                    fmt.Printf("Key in limits is updated from %s to %s\n", keyStr, newKey)
                    newLimits[newKey] = value
                } else {
                    newLimits[keyStr] = value
                }
            }
            patches = append(patches, map[string]interface{}{
                "op":    "replace",
                "path":  fmt.Sprintf("/spec/containers/%d/resources/limits", i),
                "value": newLimits,
            })
        }

        // Handle requests
        if requests := container.Resources.Requests; requests != nil {
            newRequests := make(map[string]interface{})
            for key, value := range requests {
                keyStr := string(key)
                if runtimeClassName != nil &&
                    allowedRuntimeClasses[*runtimeClassName] &&
		    strings.HasPrefix(keyStr, "cex.s390.ibm.com/") {

                    newKey := strings.Replace(keyStr, "cex.s390.ibm.com", "mdev.s390.ibm.com", 1)
                    fmt.Printf("Key in requests is updated from %s to %s\n", keyStr, newKey)
                    newRequests[newKey] = value
                } else {
                    newRequests[keyStr] = value
                }
            }
            patches = append(patches, map[string]interface{}{
                "op":    "replace",
                "path":  fmt.Sprintf("/spec/containers/%d/resources/requests", i),
                "value": newRequests,
            })
        }
    }

    // Create JSON patch response
    patchBytes, err := json.Marshal(patches)
    if err != nil {
        return &admissionv1.AdmissionResponse{
            Result: &metav1.Status{
                Message: err.Error(),
            },
        }
    }

    // Log the generated patch
    fmt.Printf("Generated Patch: %s\n", string(patchBytes))

    // Apply the patch locally to simulate the mutation
    patchedPod, err := applyPatch(raw, patchBytes)
    if err != nil {
        fmt.Printf("Error applying patch: %v\n", err)
    } else {
        fmt.Printf("Mutated Pod Spec: %s\n", string(patchedPod))
    }

    patchType := admissionv1.PatchTypeJSONPatch
    return &admissionv1.AdmissionResponse{
        Allowed:   true,
        Patch:     patchBytes,
        PatchType: &patchType,
    }
}

func applyPatch(original []byte, patch []byte) ([]byte, error) {
    patchObj, err := jsonpatch.DecodePatch(patch)
    if err != nil {
        return nil, err
    }
    return patchObj.Apply(original)
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
