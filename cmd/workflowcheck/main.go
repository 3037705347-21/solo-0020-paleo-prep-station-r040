package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
}

type checkpointView struct {
	Key         string `json:"key"`
	Status      string `json:"status"`
	Result      string `json:"result"`
	InspectedBy string `json:"inspected_by"`
}

type planSnapshot struct {
	Plan        planView         `json:"plan"`
	Checkpoints []checkpointView `json:"checkpoints"`
	Progress    []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
}

type errorView struct {
	Code string `json:"code"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to check")
	flag.Parse()

	if err := run(*workflow); err != nil {
		fmt.Fprintln(os.Stderr, "workflow check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}

func run(workflow string) error {
	handler := httpapi.NewRouter(station.New(repository.NewMemory()))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	switch workflow {
	case "register-specimen":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		if specimen.Status != "registered" {
			return fmt.Errorf("expected registered status, got %s", specimen.Status)
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.ID != specimen.ID {
			return errors.New("registered specimen cannot be read back")
		}
		return nil

	case "approve-prep-plan":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		plan, err := createPlan(client, server.URL, specimen.ID)
		if err != nil {
			return err
		}
		var approved planView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/plans/"+plan.ID+"/approval",
			contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
			http.StatusOK,
			&approved,
		); err != nil {
			return err
		}
		if approved.Status != "approved" {
			return fmt.Errorf("expected approved plan, got %s", approved.Status)
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "preparing" {
			return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "log-preparation":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 35, "矩阵剥离"); err != nil {
			return err
		}
		if err := recordInspection(client, server.URL, plan.ID, "matrix-removal", "passed"); err != nil {
			return err
		}
		if err := recordInspection(client, server.URL, plan.ID, "surface-consolidation", "passed"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var snapshot planSnapshot
		if err := requestJSON(client, http.MethodGet, server.URL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
			return err
		}
		if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 2 {
			return errors.New("plan did not complete with exactly two progress entries")
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "release-for-study":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordInspection(client, server.URL, plan.ID, "matrix-removal", "passed"); err != nil {
			return err
		}
		if err := recordInspection(client, server.URL, plan.ID, "surface-consolidation", "passed"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var release releaseView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/specimens/"+specimen.ID+"/release",
			contracts.ReleaseRequest{
				Purpose:    "比较解剖研究",
				Recipient:  "地层研究组",
				ApprovedBy: "keeper-zhou",
			},
			http.StatusCreated,
			&release,
		); err != nil {
			return err
		}
		if release.SpecimenID != specimen.ID {
			return errors.New("release record references the wrong specimen")
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.Status != "available" {
			return fmt.Errorf("expected available specimen, got %s", fetched.Status)
		}
		return nil

	case "stage-inspection":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		progressURL := server.URL + "/plans/" + plan.ID + "/progress"
		inspectionsURL := server.URL + "/plans/" + plan.ID + "/inspections"
		// 达到检查点阈值允许登记，越过未通过的检查点被拒绝。
		if err := recordProgress(client, server.URL, plan.ID, 35, "矩阵剥离"); err != nil {
			return err
		}
		if err := expectConflict(client, progressURL, progressBody(50, "继续剥离"), "checkpoint_not_passed"); err != nil {
			return err
		}
		// 前置检查点未通过时，后续检查点不能登记。
		if err := expectConflict(client, inspectionsURL, inspectionBody("surface-consolidation", "passed"), "checkpoint_out_of_order"); err != nil {
			return err
		}
		// 退回结论会阻断后续进度，直到重新检查通过。
		if err := recordInspection(client, server.URL, plan.ID, "matrix-removal", "rejected"); err != nil {
			return err
		}
		if err := expectConflict(client, progressURL, progressBody(40, "返工剥离"), "checkpoint_rejected"); err != nil {
			return err
		}
		if err := recordInspection(client, server.URL, plan.ID, "matrix-removal", "passed"); err != nil {
			return err
		}
		// 已通过的检查点不能重复检查。
		if err := expectConflict(client, inspectionsURL, inspectionBody("matrix-removal", "passed"), "checkpoint_already_passed"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 60, "粗修成形"); err != nil {
			return err
		}
		// 最终检查通过前不能完成方案。
		if err := expectConflict(client, progressURL, progressBody(100, "表面加固"), "final_checkpoint_not_passed"); err != nil {
			return err
		}
		if err := recordInspection(client, server.URL, plan.ID, "surface-consolidation", "passed"); err != nil {
			return err
		}
		// 并发完成请求必须恰好成功一个，其余得到确定的冲突结果。
		if err := completeConcurrently(client, progressURL); err != nil {
			return err
		}
		var snapshot planSnapshot
		if err := requestJSON(client, http.MethodGet, server.URL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
			return err
		}
		if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 3 {
			return errors.New("plan did not complete with exactly three progress entries")
		}
		if len(snapshot.Checkpoints) != 2 {
			return errors.New("plan snapshot must expose both checkpoints")
		}
		for _, checkpoint := range snapshot.Checkpoints {
			if checkpoint.Status != "passed" || checkpoint.Result != "passed" || checkpoint.InspectedBy != "qa-wang" {
				return fmt.Errorf("checkpoint %s missing public inspection record", checkpoint.Key)
			}
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return nil
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func registerSpecimen(client *http.Client, baseURL string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: "PPS-2026-001",
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func createPlan(client *http.Client, baseURL, specimenID string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
			Checkpoints: []contracts.CheckpointRequest{
				{Key: "matrix-removal", Name: "矩阵剥离", GatePercent: 35},
				{Key: "surface-consolidation", Name: "表面加固", GatePercent: 100},
			},
		},
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func completedPlanSetup(client *http.Client, baseURL string) (specimenView, planView, error) {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		&approved,
	); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		progressBody(percent, stage),
		http.StatusCreated,
		&ignored,
	)
}

func progressBody(percent int, stage string) contracts.ProgressRequest {
	return contracts.ProgressRequest{
		Stage:      stage,
		Percent:    percent,
		Note:       "本地链路检查",
		RecordedBy: "preparator-chen",
	}
}

func recordInspection(client *http.Client, baseURL, planID, checkpointKey, result string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/inspections",
		inspectionBody(checkpointKey, result),
		http.StatusCreated,
		&ignored,
	)
}

func inspectionBody(checkpointKey, result string) contracts.InspectionRequest {
	return contracts.InspectionRequest{
		CheckpointKey: checkpointKey,
		Result:        result,
		InspectedBy:   "qa-wang",
		Note:          "阶段质量确认",
	}
}

func expectConflict(client *http.Client, url string, body any, wantCode string) error {
	var failure errorView
	if err := requestJSON(client, http.MethodPost, url, body, http.StatusConflict, &failure); err != nil {
		return err
	}
	if failure.Code != wantCode {
		return fmt.Errorf("expected conflict code %s, got %s", wantCode, failure.Code)
	}
	return nil
}

func completeConcurrently(client *http.Client, progressURL string) error {
	const attempts = 5
	statuses := make(chan int, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := postStatus(client, progressURL, progressBody(100, "表面加固"))
			if err != nil {
				status = -1
			}
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	succeeded := 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			succeeded++
		case http.StatusConflict:
		default:
			return fmt.Errorf("unexpected concurrent progress status %d", status)
		}
	}
	if succeeded != 1 {
		return fmt.Errorf("expected exactly one concurrent completion, got %d", succeeded)
	}
	return nil
}

func postStatus(client *http.Client, url string, body any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}
