package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"microwaveplanner/internal/assessment"
	"microwaveplanner/internal/store"
	"microwaveplanner/internal/task"
)

func newTestServer(t *testing.T) (*ginRouterHandle, string, func()) {
	t.Helper()
	dsn := "root@tcp(127.0.0.1:3306)/"
	name := fmt.Sprintf("mw_http_%d_%04d", time.Now().UnixNano(), rand.Intn(10000))
	st, err := store.NewWithDatabase(dsn, name)
	if err != nil {
		t.Skipf("integration database unavailable (%v)", err)
	}
	svc := assessment.New(st)
	mgr := task.NewManager(st, svc)
	router := NewRouter(&Server{Store: st, Assessment: svc, Tasks: mgr})
	ts := httptest.NewServer(router)
	return &ginRouterHandle{
			ts: ts, st: st, svc: svc, mgr: mgr,
		}, ts.URL, func() {
			ts.Close()
			_ = st.Close()
		}
}

type ginRouterHandle struct {
	ts  *httptest.Server
	st  *store.Store
	svc *assessment.Service
	mgr *task.Manager
}

func (h *ginRouterHandle) runQueued(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for {
		id, err := h.st.OldestQueuedTask(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if id == "" {
			return
		}
		if err := h.mgr.RunOneForTest(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *ginRouterHandle) do(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, h.ts.URL+"/api/v1"+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	dec := json.NewDecoder(resp.Body)
	_ = dec.Decode(&out)
	if out == nil {
		out = map[string]any{}
	}
	return resp.StatusCode, out
}

// publish is a small helper that PUTs a draft and publishes it.
func (h *ginRouterHandle) publish(t *testing.T, layer, kind, id string, fields map[string]any) {
	t.Helper()
	if code, out := h.do(t, http.MethodPut,
		fmt.Sprintf("/layers/%s/scopes/%s/%s/draft", layer, kind, id),
		map[string]any{"baseRevision": 0, "fields": fields}); code != http.StatusOK {
		t.Fatalf("draft %s/%s/%s code=%d body=%v", layer, kind, id, code, out)
	}
	if code, out := h.do(t, http.MethodPost,
		fmt.Sprintf("/layers/%s/scopes/%s/%s/publish", layer, kind, id),
		map[string]any{}); code != http.StatusAccepted {
		t.Fatalf("publish %s/%s/%s code=%d body=%v", layer, kind, id, code, out)
	}
}

// TestHTTPEndToEnd drives the public API: provenance, field errors, evaluation,
// publish/impact task with diff, and optimistic-conflict status.
func TestHTTPEndToEnd(t *testing.T) {
	h, _, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	p := fmt.Sprintf("h%d", time.Now().UnixNano())

	radioFields := map[string]any{
		"radio.txPowerDbm": 15, "radio.rxThresholdDbm": -85,
		"radio.minFrequencyGHz": 17.5, "radio.maxFrequencyGHz": 18.7,
		"radio.feederLossDbPerM": 0.12, "radio.splitterLossDb": 0,
	}
	h.publish(t, "nominal", "radio_model", p+"_radio", radioFields)
	h.publish(t, "nominal", "antenna_model", p+"_ant", map[string]any{"antenna.gainDb": 38})
	h.do(t, http.MethodPost, "/rain-climates", map[string]any{"id": p + "_rain", "name": "r", "r001MMh": 30})
	h.do(t, http.MethodPost, "/rain-coefficients", map[string]any{
		"id": p + "_coef", "bandId": p + "_band", "frequencyGHz": 18.1,
		"polarization": "H", "k": 0.07, "alpha": 1.08,
	})
	h.do(t, http.MethodPost, "/sites", map[string]any{"id": p + "_s1", "name": "s1", "rainClimateRegionId": p + "_rain"})
	h.do(t, http.MethodPost, "/sites", map[string]any{"id": p + "_s2", "name": "s2", "rainClimateRegionId": p + "_rain"})
	if code, out := h.do(t, http.MethodPost, "/endpoints", map[string]any{
		"id": p + "_e1", "siteId": p + "_s1", "name": "e1",
		"radioModelId": p + "_radio", "antennaModelId": p + "_ant",
	}); code != http.StatusCreated {
		t.Fatalf("create e1: %d %v", code, out)
	}
	if code, out := h.do(t, http.MethodPost, "/endpoints", map[string]any{
		"id": p + "_e2", "siteId": p + "_s2", "name": "e2",
		"radioModelId": p + "_radio", "antennaModelId": p + "_ant",
	}); code != http.StatusCreated {
		t.Fatalf("create e2: %d %v", code, out)
	}
	h.publish(t, "site", "endpoint_radio", p+"_e1", map[string]any{"installation.feederLengthM": 30})
	h.publish(t, "site", "endpoint_radio", p+"_e2", map[string]any{"installation.feederLengthM": 30})
	h.runQueued(t)

	// Frequency outside supported band must be a field error.
	code, out := h.do(t, http.MethodPost, "/links", map[string]any{
		"id": p + "_bad", "name": "bad", "endAId": p + "_e1", "endBId": p + "_e2",
		"frequencyGHz": 20, "distanceKm": 10, "polarization": "H",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("out-of-band frequency code=%d body=%v", code, out)
	}
	fields, _ := out["fields"].([]any)
	if len(fields) == 0 || !strings.Contains(fmt.Sprint(fields[0]), "frequencyBand") {
		t.Fatalf("expected frequencyBand field error, got %v", fields)
	}

	// Non-positive distance must be reported.
	code, out = h.do(t, http.MethodPost, "/links", map[string]any{
		"id": p + "_bad2", "name": "bad2", "endAId": p + "_e1", "endBId": p + "_e2",
		"frequencyGHz": 18.1, "distanceKm": 0, "polarization": "H",
	})
	if code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out), "distanceKm") {
		t.Fatalf("distance error expected, got %d %v", code, out)
	}

	// Referencing a nonexistent model fails.
	code, _ = h.do(t, http.MethodPost, "/endpoints", map[string]any{
		"id": p + "_ex", "siteId": p + "_s1", "name": "ex",
		"radioModelId": p + "_missing", "antennaModelId": p + "_ant",
	})
	if code != http.StatusNotFound && code != http.StatusBadRequest {
		t.Fatalf("missing model reference code=%d", code)
	}

	h.do(t, http.MethodPost, "/links", map[string]any{
		"id": p + "_l1", "name": "l1", "endAId": p + "_e1", "endBId": p + "_e2",
		"frequencyGHz": 18.1, "distanceKm": 10, "polarization": "H",
		"rainRegionId": p + "_rain", "rainBandId": p + "_band", "targetAvailability": 0.9999,
	})

	// Effective parameter provenance must attribute fields to the right layer.
	code, out = h.do(t, http.MethodGet, "/endpoints/"+p+"_e1/effective", nil)
	if code != http.StatusOK {
		t.Fatalf("effective code=%d %v", code, out)
	}
	eff := out["effective"].(map[string]any)
	radio := eff["radio"].(map[string]any)["fields"].(map[string]any)
	tx := radio["radio.txPowerDbm"].(map[string]any)
	if tx["source"].(map[string]any)["layer"] != "nominal" {
		t.Fatalf("tx power must come from nominal, got %v", tx["source"])
	}
	feeder := radio["installation.feederLengthM"].(map[string]any)
	if feeder["source"].(map[string]any)["layer"] != "site" {
		t.Fatalf("feeder length must come from site, got %v", feeder["source"])
	}

	// Instant evaluation succeeds and has a positive margin.
	code, out = h.do(t, http.MethodPost, "/links/"+p+"_l1/evaluate", nil)
	if code != http.StatusOK {
		t.Fatalf("evaluate code=%d %v", code, out)
	}
	budget := out["budget"].(map[string]any)
	if budget["fadeMarginDb"].(float64) <= 0 {
		t.Fatalf("baseline margin must be positive, got %v", budget["fadeMarginDb"])
	}

	// Two concurrent writers of the same fresh draft: the stale one gets 409.
	h.do(t, http.MethodPut, "/layers/nominal/scopes/radio_model/"+p+"_conf/draft",
		map[string]any{"baseRevision": 0, "fields": radioFields})
	code, out = h.do(t, http.MethodPut, "/layers/nominal/scopes/radio_model/"+p+"_conf/draft",
		map[string]any{"baseRevision": 0, "fields": radioFields})
	if code != http.StatusConflict || out["error"] != "draft_conflict" {
		t.Fatalf("expected 409 draft_conflict, got %d %v", code, out)
	}

	// Publish a real nominal power reduction: the returned task lists exactly
	// the two endpoints / one link; after running, margin drops by 10 dB.
	h.publish(t, "nominal", "radio_model", p+"_radio", map[string]any{
		"radio.txPowerDbm": 5, "radio.rxThresholdDbm": -85,
		"radio.minFrequencyGHz": 17.5, "radio.maxFrequencyGHz": 18.7,
		"radio.feederLossDbPerM": 0.12, "radio.splitterLossDb": 0,
	})
	// mustPublish generated the task but it is queued; publish helper does not
	// return id. Drain and inspect the latest task directly.
	tasks, err := h.st.ListTasks(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var taskID string
	for _, tk := range tasks {
		if tk.Trigger == "publish" && len(tk.Affected.Links) == 1 && tk.Affected.Links[0] == p+"_l1" {
			taskID = tk.ID
			break
		}
	}
	if taskID == "" {
		t.Fatalf("expected one publish task affecting exactly l1, got %+v", tasks)
	}
	h.runQueued(t)
	done, err := h.st.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(done.Diff.MarginDecreased) != 1 {
		t.Fatalf("expected one decreased margin, got %+v", done.Diff.MarginDecreased)
	}
	if d := done.Diff.MarginDecreased[0].DeltaDB; d > -10+1e-9 || d < -10-1e-9 {
		t.Fatalf("margin delta = %v, want -10 dB", d)
	}

	// History endpoint returns 200; replay is then exercised by id.
	listCode, _ := h.do(t, http.MethodGet, "/links/"+p+"_l1/evaluations", nil)
	if listCode != http.StatusOK {
		t.Fatalf("history code=%d", listCode)
	}
	// Fetch the newest instant evaluation id deterministically from the store.
	stored, err := h.st.ListLinkEvaluations(ctx, p+"_l1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 {
		t.Fatal("expected persisted instant evaluation history")
	}
	var instantID string
	for _, e := range stored {
		if e.Kind == "instant" {
			instantID = e.ID
			break
		}
	}
	if instantID == "" {
		t.Fatal("no instant evaluation found in history")
	}
	replayCode, replayOut := h.do(t, http.MethodPost, "/link-evaluations/"+instantID+"/replay", nil)
	if replayCode != http.StatusOK || replayOut["reproducible"] != true {
		t.Fatalf("replay code=%d out=%v", replayCode, replayOut)
	}
}
