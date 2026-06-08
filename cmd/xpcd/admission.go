package main

// Minimal Kubernetes admission-webhook wire types (admission.k8s.io/v1).
//
// xpcd speaks the AdmissionReview protocol directly over net/http rather than
// pulling in client-go: the request and response are plain JSON, and the only
// fields xpcd needs are the embedded object, its GroupVersionKind/identity, and
// the allow/deny response. Keeping this self-contained means the runtime daemon
// inherits the same zero-heavy-dependency posture as the static xpc binary.

import "encoding/json"

// AdmissionReview is the top-level envelope exchanged with the API server. The
// same struct is used for both the inbound request and the outbound response;
// the API server populates Request, and the webhook populates Response.
type AdmissionReview struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Request    *AdmissionRequest  `json:"request,omitempty"`
	Response   *AdmissionResponse `json:"response,omitempty"`
}

// GroupVersionKind identifies the type of the admitted object.
type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// AdmissionRequest is the API server's description of the operation under
// review. Object carries the raw manifest bytes xpcd type-checks; for DELETE
// operations Object is empty and OldObject holds the resource being removed.
type AdmissionRequest struct {
	UID       string           `json:"uid"`
	Kind      GroupVersionKind `json:"kind"`
	Name      string           `json:"name"`
	Namespace string           `json:"namespace"`
	Operation string           `json:"operation"`
	Object    json.RawMessage  `json:"object,omitempty"`
	OldObject json.RawMessage  `json:"oldObject,omitempty"`
}

// AdmissionResponse is xpcd's verdict. UID must echo the request UID. Warnings
// surface non-blocking diagnostics to the API client even in enforce mode and
// are the primary channel in audit mode, where Allowed is always true.
type AdmissionResponse struct {
	UID      string           `json:"uid"`
	Allowed  bool             `json:"allowed"`
	Status   *AdmissionStatus `json:"status,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
}

// AdmissionStatus carries the human-readable reason shown when a request is
// denied (mapped into the API server's 4xx surfaced to `kubectl apply`).
type AdmissionStatus struct {
	Code    int32  `json:"code,omitempty"`
	Message string `json:"message"`
}

// reviewResponse wraps an AdmissionResponse back into the envelope the API
// server expects, echoing the request's apiVersion/kind.
func reviewResponse(reqAPIVersion, reqKind string, resp *AdmissionResponse) AdmissionReview {
	apiVersion := reqAPIVersion
	if apiVersion == "" {
		apiVersion = "admission.k8s.io/v1"
	}
	kind := reqKind
	if kind == "" {
		kind = "AdmissionReview"
	}
	return AdmissionReview{
		APIVersion: apiVersion,
		Kind:       kind,
		Response:   resp,
	}
}

// allowResponse builds an allow verdict carrying optional warnings.
func allowResponse(uid string, warnings []string) *AdmissionResponse {
	return &AdmissionResponse{UID: uid, Allowed: true, Warnings: warnings}
}

// denyResponse builds a deny verdict with a reason and optional warnings.
func denyResponse(uid, message string, warnings []string) *AdmissionResponse {
	return &AdmissionResponse{
		UID:      uid,
		Allowed:  false,
		Status:   &AdmissionStatus{Code: 403, Message: message},
		Warnings: warnings,
	}
}
