// Copyright 2026 DataRobot, Inc. and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package drapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/datarobot/cli/internal/config"
	"github.com/datarobot/cli/internal/log"
)

// LLM source kinds. Gateway models come from the LLM Gateway catalog; deployed
// models are DataRobot Deployments whose champion model is a TextGeneration
// (chat) model, used on-prem or wherever the gateway catalog is empty.
const (
	LLMKindGateway  = "gateway"
	LLMKindDeployed = "deployed"
	LLMKindLiteLLM  = "litellm"

	// deployedModelSentinel is the litellm model string a deployed LLM is
	// addressed by; the deployment id carries the actual routing. Matches the
	// deployed-model .env contract consumed by downstream tooling.
	deployedModelSentinel = "datarobot/datarobot-deployed-llm"

	// targetTypeTextGeneration is the champion-model target type that marks a
	// deployment as a chat LLM.
	targetTypeTextGeneration = "TextGeneration"

	liteLLMBaseURLEnv = "LITELLM_BASE_URL"
	liteLLMAPIKeyEnv  = "LITELLM_API_KEY"
)

type LLM struct {
	LlmID    string `json:"llmId"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	IsActive bool   `json:"isActive"`
	Model    string `json:"model"`

	Description string `json:"description"`
	ContextSize int    `json:"contextSize"`

	// Kind and DeploymentID are set programmatically, not decoded from any API:
	// gateway rows are LLMKindGateway; deployed rows are LLMKindDeployed and
	// carry the deployment id.
	Kind         string `json:"-"`
	DeploymentID string `json:"-"`

	//Version              string   `json:"version"`
	//Creator              string   `json:"creator"`
	//MaxCompletionTokens  int      `json:"maxCompletionTokens"`
	//Capabilities         []string `json:"capabilities"`
	//SupportedLanguages   []string `json:"supportedLanguages"`
	//InputTypes           []string `json:"inputTypes"`
	//OutputTypes          []string `json:"outputTypes"`
	//DocumentationLink    string   `json:"documentationLink"`
	//DateAdded            string   `json:"dateAdded"`
	//License              string   `json:"license"`
	//IsPreview            bool     `json:"isPreview"`
	//IsMetered            bool     `json:"isMetered"`
	//RetirementDate       string   `json:"retirementDate"`
	//SuggestedReplacement string   `json:"suggestedReplacement"`
	//IsDeprecated         bool     `json:"isDeprecated"`
	//AvailableRegions     []string `json:"availableRegions"`
	//
	//ReferenceLinks []struct {
	//	Name string `json:"name"`
	//	URL  string `json:"url"`
	//} `json:"referenceLinks"`
	//
	//AvailableLitellmEndpoints struct {
	//	SupportsChatCompletions bool `json:"supportsChatCompletions"`
	//	SupportsResponses       bool `json:"supportsResponses"`
	//} `json:"availableLitellmEndpoints"`
}

type LLMList struct {
	LLMs       []LLM  `json:"data"`
	Count      int    `json:"count"`
	TotalCount int    `json:"totalCount"`
	Next       string `json:"next"`
	Previous   string `json:"previous"`

	// Warnings is set programmatically (not decoded) by GetLLMsAndDeployed when
	// one source failed but the other succeeded, so callers can surface why the
	// list is partial instead of silently treating a missing source as empty.
	Warnings []string `json:"-"`
}

func GetLLMs() (*LLMList, error) {
	url, err := config.GetEndpointURL("/api/v2/genai/llmgw/catalog/?limit=100")
	if err != nil {
		return nil, err
	}

	var llmList LLMList

	var active []LLM

	for url != "" {
		llmList = LLMList{}

		err = GetJSON(url, "LLMs", &llmList)
		if err != nil {
			return nil, err
		}

		for _, llm := range llmList.LLMs {
			if llm.IsActive {
				llm.Kind = LLMKindGateway
				active = append(active, llm)
			}
		}

		if llmList.Next == "" {
			break
		}

		if err = AssertNextOnSameHost(llmList.Next); err != nil {
			return nil, err
		}

		url = llmList.Next
	}

	llmList.LLMs = active

	return &llmList, nil
}

// deployment is the subset of the /api/v2/deployments/ response the CLI needs
// to present a deployed model as a selectable LLM.
type deployment struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Model       struct {
		TargetType string `json:"targetType"`
	} `json:"model"`
}

type deploymentList struct {
	Data       []deployment `json:"data"`
	Count      int          `json:"count"`
	TotalCount int          `json:"totalCount"`
	Next       string       `json:"next"`
	Previous   string       `json:"previous"`
}

// GetDeployedLLMs lists DataRobot Deployments serving as chat LLMs (champion
// model target type TextGeneration). The server-side championModelTargetType
// filter is honored on recent platforms; older on-prem builds ignore unknown
// query params and return every deployment, so rows are re-filtered
// client-side on target type and active status.
func GetDeployedLLMs() ([]LLM, error) {
	url, err := config.GetEndpointURL("/api/v2/deployments/?championModelTargetType=" + targetTypeTextGeneration + "&limit=100")
	if err != nil {
		return nil, err
	}

	var deployed []LLM

	for url != "" {
		var dl deploymentList

		if err = GetJSON(url, "deployed LLMs", &dl); err != nil {
			return nil, err
		}

		for _, d := range dl.Data {
			if d.Model.TargetType != targetTypeTextGeneration || !strings.EqualFold(d.Status, "active") {
				continue
			}

			// A deployment's label is nullable; fall back to its id so the name
			// column is never blank.
			name := d.Label
			if name == "" {
				name = d.ID
			}

			deployed = append(deployed, LLM{
				LlmID:        d.ID,
				Name:         name,
				Model:        deployedModelSentinel,
				Description:  d.Description,
				IsActive:     true,
				Kind:         LLMKindDeployed,
				DeploymentID: d.ID,
			})
		}

		if dl.Next == "" {
			break
		}

		if err = AssertNextOnSameHost(dl.Next); err != nil {
			return nil, err
		}

		url = dl.Next
	}

	return deployed, nil
}

// liteLLMModel is the subset of LiteLLM's /models response used in the CLI.
// The LiteLLM OpenAI-compatible API identifies a model with id and optionally
// reports its provider via owned_by.
type liteLLMModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type liteLLMModelList struct {
	Data []liteLLMModel `json:"data"`
}

// LiteLLMConfigured reports whether both credentials required to query the
// standalone LiteLLM endpoint are available.
func LiteLLMConfigured() bool {
	return os.Getenv(liteLLMBaseURLEnv) != "" && os.Getenv(liteLLMAPIKeyEnv) != ""
}

// GetLiteLLMLLMs lists models exposed by the configured LiteLLM proxy.
func GetLiteLLMLLMs() ([]LLM, error) {
	baseURL := strings.TrimRight(os.Getenv(liteLLMBaseURLEnv), "/")

	apiKey := os.Getenv(liteLLMAPIKeyEnv)

	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("LiteLLM requires %s and %s", liteLLMBaseURLEnv, liteLLMAPIKeyEnv)
	}

	url := baseURL + "/models"

	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:gosec // LiteLLM endpoint is explicitly configured by the user.
	if err != nil {
		return nil, fmt.Errorf("create LiteLLM models request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := NewHTTPClient(0).Do(req) //nolint:gosec // LiteLLM endpoint is explicitly configured by the user.
	if err != nil {
		return nil, fmt.Errorf("request LiteLLM models: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode, URL: url}
	}

	var models liteLLMModelList
	if err = json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("decode LiteLLM models: %w", err)
	}

	llms := make([]LLM, 0, len(models.Data))
	for _, model := range models.Data {
		llms = append(llms, LLM{
			LlmID:    model.ID,
			Name:     model.ID,
			Provider: model.OwnedBy,
			IsActive: true,
			Model:    model.ID,
			Kind:     LLMKindLiteLLM,
		})
	}

	return llms, nil
}

// GetLLMsAndDeployed returns the union of LLM Gateway catalog models,
// DataRobot-deployed LLMs, and configured LiteLLM models. Each source is
// best-effort: a single-source failure is logged and the other source is still
// returned. LiteLLM is queried only when both of its required environment
// variables are set. An error is returned only when every queried source fails.
//
// The sources are fetched concurrently. Each is a paginated round-trip set, so
// the caller waits on the slower one instead of both.
//
// Keep this call outside a running TUI. Both goroutines log through
// internal/log, whose stderrLogger global tui.Run rewrites. Nothing overlaps
// today because select starts its picker after this returns, but wrapping the
// fetch in a spinner would make that a live race.
func GetLLMsAndDeployed() (*LLMList, error) {
	var (
		gateway  *LLMList
		deployed []LLM
		liteLLM  []LLM
		gwErr    error
		depErr   error
		liteErr  error
		wg       sync.WaitGroup
	)

	wg.Add(2)

	if LiteLLMConfigured() {
		wg.Add(1)

		go func() {
			defer wg.Done()

			liteLLM, liteErr = GetLiteLLMLLMs()
		}()
	}

	go func() {
		defer wg.Done()

		gateway, gwErr = GetLLMs()
	}()

	go func() {
		defer wg.Done()

		deployed, depErr = GetDeployedLLMs()
	}()

	wg.Wait()

	var warnings []string

	if gwErr != nil {
		warnings = append(warnings, "LLM Gateway catalog unavailable: "+gwErr.Error())

		log.Warnf("Could not list LLM Gateway models: %s", gwErr.Error())
	}

	if depErr != nil {
		warnings = append(warnings, "DataRobot-deployed LLMs unavailable: "+depErr.Error())

		log.Warnf("Could not list DataRobot-deployed LLMs: %s", depErr.Error())
	}

	if liteErr != nil {
		warnings = append(warnings, "LiteLLM models unavailable: "+liteErr.Error())

		log.Warnf("Could not list LiteLLM models: %s", liteErr.Error())
	}

	errs := []error{gwErr, depErr}
	if LiteLLMConfigured() {
		errs = append(errs, liteErr)
	}

	allFailed := true

	for _, err := range errs {
		if err == nil {
			allFailed = false

			break
		}
	}

	if allFailed {
		return nil, errors.Join(errs...)
	}

	var llms []LLM

	if gateway != nil {
		llms = append(llms, gateway.LLMs...)
	}

	llms = append(llms, deployed...)
	llms = append(llms, liteLLM...)

	return &LLMList{LLMs: llms, Count: len(llms), TotalCount: len(llms), Warnings: warnings}, nil
}
