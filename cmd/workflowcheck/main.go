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

type planSnapshot struct {
	Plan        planView `json:"plan"`
	Checkpoints []struct {
		Stage string `json:"stage"`
		Final bool   `json:"final"`
		State string `json:"state"`
	} `json:"checkpoints"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
	Inspections []struct {
		Checkpoint string `json:"checkpoint"`
		Outcome    string `json:"outcome"`
	} `json:"inspections"`
}

type errorView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
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
		return checkStageInspection(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func checkStageInspection(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return err
	}
	var plan planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
			Checkpoints: []contracts.CheckpointRequest{
				{Stage: "矩阵剥离", Percent: 35},
				{Stage: "表面加固", Percent: 100},
			},
		},
		http.StatusCreated,
		&plan,
	); err != nil {
		return err
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
		return err
	}
	plan = approved

	// 未触及检查点的进度可以直接记录。
	if err := recordProgress(client, baseURL, plan.ID, 20, "围岩清理"); err != nil {
		return err
	}
	// 未通过检查前，进度不能越过检查点。
	if err := expectConflict(client, baseURL+"/plans/"+plan.ID+"/progress",
		contracts.ProgressRequest{Stage: "矩阵剥离", Percent: 40, Note: "本地链路检查", RecordedBy: "preparator-chen"},
		"checkpoint_not_passed"); err != nil {
		return err
	}
	// 错误顺序的检查被拒绝：后一个检查点不能先检。
	if err := expectConflict(client, baseURL+"/plans/"+plan.ID+"/inspections",
		contracts.InspectionRequest{Checkpoint: "表面加固", Outcome: "passed", InspectedBy: "quality-wu"},
		"inspection_out_of_order"); err != nil {
		return err
	}
	// 退回后进度仍然受阻，只有重新检查通过才能继续。
	if err := recordInspection(client, baseURL, plan.ID, "矩阵剥离", "rejected"); err != nil {
		return err
	}
	if err := expectConflict(client, baseURL+"/plans/"+plan.ID+"/progress",
		contracts.ProgressRequest{Stage: "矩阵剥离", Percent: 40, Note: "本地链路检查", RecordedBy: "preparator-chen"},
		"checkpoint_not_passed"); err != nil {
		return err
	}
	if err := recordInspection(client, baseURL, plan.ID, "矩阵剥离", "passed"); err != nil {
		return err
	}
	// 重复检查同一已通过检查点被拒绝。
	if err := expectConflict(client, baseURL+"/plans/"+plan.ID+"/inspections",
		contracts.InspectionRequest{Checkpoint: "矩阵剥离", Outcome: "passed", InspectedBy: "quality-wu"},
		"checkpoint_already_passed"); err != nil {
		return err
	}
	if err := recordProgress(client, baseURL, plan.ID, 40, "矩阵剥离"); err != nil {
		return err
	}
	// 最终检查未通过前，方案不能完成。
	if err := expectConflict(client, baseURL+"/plans/"+plan.ID+"/progress",
		contracts.ProgressRequest{Stage: "表面加固", Percent: 100, Note: "本地链路检查", RecordedBy: "preparator-chen"},
		"checkpoint_not_passed"); err != nil {
		return err
	}
	if err := recordInspection(client, baseURL, plan.ID, "表面加固", "passed"); err != nil {
		return err
	}
	// 并发完成请求必须有确定结果：恰好一个成功、一个冲突，不留半截状态。
	if err := raceFinalProgress(client, baseURL, plan.ID); err != nil {
		return err
	}

	var snapshot planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
		return err
	}
	if snapshot.Plan.Status != "completed" {
		return fmt.Errorf("expected completed plan, got %s", snapshot.Plan.Status)
	}
	if len(snapshot.Progress) != 3 || snapshot.Progress[len(snapshot.Progress)-1].Percent != 100 {
		return errors.New("plan must hold exactly three progress entries ending at one hundred")
	}
	if len(snapshot.Inspections) != 3 {
		return fmt.Errorf("expected three inspection records, got %d", len(snapshot.Inspections))
	}
	if len(snapshot.Checkpoints) != 2 || !snapshot.Checkpoints[1].Final {
		return errors.New("checkpoint projection must expose two checkpoints with a final one")
	}
	for _, checkpoint := range snapshot.Checkpoints {
		if checkpoint.State != "passed" {
			return fmt.Errorf("checkpoint %s must be passed, got %s", checkpoint.Stage, checkpoint.State)
		}
	}
	// 方案完成后不能再记录检查。
	if err := expectConflict(client, baseURL+"/plans/"+plan.ID+"/inspections",
		contracts.InspectionRequest{Checkpoint: "矩阵剥离", Outcome: "passed", InspectedBy: "quality-wu"},
		"plan_not_inspectable"); err != nil {
		return err
	}
	var fetched specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.Status != "stabilized" {
		return fmt.Errorf("expected stabilized specimen, got %s", fetched.Status)
	}
	return nil
}

func recordInspection(client *http.Client, baseURL, planID, checkpoint, outcome string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/inspections",
		contracts.InspectionRequest{
			Checkpoint:  checkpoint,
			Outcome:     outcome,
			InspectedBy: "quality-wu",
			Note:        "本地链路检查",
		},
		http.StatusCreated,
		&ignored,
	)
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

func raceFinalProgress(client *http.Client, baseURL, planID string) error {
	type result struct {
		status int
		err    error
	}
	results := make(chan result, 2)
	body := contracts.ProgressRequest{
		Stage:      "表面加固",
		Percent:    100,
		Note:       "并发完成检查",
		RecordedBy: "preparator-chen",
	}
	for i := 0; i < 2; i++ {
		go func() {
			status, err := postStatus(client, baseURL+"/plans/"+planID+"/progress", body)
			results <- result{status: status, err: err}
		}()
	}
	created := 0
	conflicted := 0
	for i := 0; i < 2; i++ {
		res := <-results
		if res.err != nil {
			return res.err
		}
		switch res.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		default:
			return fmt.Errorf("unexpected status %d from concurrent progress", res.status)
		}
	}
	if created != 1 || conflicted != 1 {
		return fmt.Errorf("expected one created and one conflict, got %d created and %d conflicted", created, conflicted)
	}
	return nil
}

func postStatus(client *http.Client, url string, body any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	response, err := client.Post(url, "application/json", bytes.NewReader(encoded))
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, nil
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
		contracts.ProgressRequest{
			Stage:      stage,
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: "preparator-chen",
		},
		http.StatusCreated,
		&ignored,
	)
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
