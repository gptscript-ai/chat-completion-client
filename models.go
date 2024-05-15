package openai

import (
	"context"
	"net/http"
)

// GPT3 Defines the models provided by OpenAI to use when generating
// completions from OpenAI.
// GPT3 Models are designed for text-based tasks. For code-specific
// tasks, please refer to the Codex series of models.
const (
	GPT432K0613           = "gpt-4-32k-0613"
	GPT432K0314           = "gpt-4-32k-0314"
	GPT432K               = "gpt-4-32k"
	GPT40613              = "gpt-4-0613"
	GPT40314              = "gpt-4-0314"
	GPT4Turbo             = "gpt-4-turbo"
	GPT4Turbo20240409     = "gpt-4-turbo-2024-04-09"
	GPT4o                 = "gpt-4o"
	GPT4o20240513         = "gpt-4o-2024-05-13"
	GPT4Turbo0125         = "gpt-4-0125-preview"
	GPT4Turbo1106         = "gpt-4-1106-preview"
	GPT4TurboPreview      = "gpt-4-turbo-preview"
	GPT4VisionPreview     = "gpt-4-vision-preview"
	GPT4                  = "gpt-4"
	GPT3Dot5Turbo0125     = "gpt-3.5-turbo-0125"
	GPT3Dot5Turbo1106     = "gpt-3.5-turbo-1106"
	GPT3Dot5Turbo0613     = "gpt-3.5-turbo-0613"
	GPT3Dot5Turbo0301     = "gpt-3.5-turbo-0301"
	GPT3Dot5Turbo16K      = "gpt-3.5-turbo-16k"
	GPT3Dot5Turbo16K0613  = "gpt-3.5-turbo-16k-0613"
	GPT3Dot5Turbo         = "gpt-3.5-turbo"
	GPT3Dot5TurboInstruct = "gpt-3.5-turbo-instruct"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3TextDavinci003 = "text-davinci-003"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3TextDavinci002 = "text-davinci-002"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3TextCurie001 = "text-curie-001"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3TextBabbage001 = "text-babbage-001"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3TextAda001 = "text-ada-001"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3TextDavinci001 = "text-davinci-001"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3DavinciInstructBeta = "davinci-instruct-beta"
	GPT3Davinci             = "davinci"
	GPT3Davinci002          = "davinci-002"
	// Deprecated: Will be shut down on January 04, 2024. Use gpt-3.5-turbo-instruct instead.
	GPT3CurieInstructBeta = "curie-instruct-beta"
	GPT3Curie             = "curie"
	GPT3Curie002          = "curie-002"
	GPT3Ada               = "ada"
	GPT3Ada002            = "ada-002"
	GPT3Babbage           = "babbage"
	GPT3Babbage002        = "babbage-002"
)

// Model struct represents an OpenAPI model.
type Model struct {
	CreatedAt  int64        `json:"created"`
	ID         string       `json:"id"`
	Object     string       `json:"object"`
	OwnedBy    string       `json:"owned_by"`
	Permission []Permission `json:"permission"`
	Root       string       `json:"root"`
	Parent     string       `json:"parent"`

	httpHeader
}

// Permission struct represents an OpenAPI permission.
type Permission struct {
	CreatedAt          int64       `json:"created"`
	ID                 string      `json:"id"`
	Object             string      `json:"object"`
	AllowCreateEngine  bool        `json:"allow_create_engine"`
	AllowSampling      bool        `json:"allow_sampling"`
	AllowLogprobs      bool        `json:"allow_logprobs"`
	AllowSearchIndices bool        `json:"allow_search_indices"`
	AllowView          bool        `json:"allow_view"`
	AllowFineTuning    bool        `json:"allow_fine_tuning"`
	Organization       string      `json:"organization"`
	Group              interface{} `json:"group"`
	IsBlocking         bool        `json:"is_blocking"`
}

// FineTuneModelDeleteResponse represents the deletion status of a fine-tuned model.
type FineTuneModelDeleteResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Deleted bool   `json:"deleted"`

	httpHeader
}

// ModelsList is a list of models, including those that belong to the user or organization.
type ModelsList struct {
	Models []Model `json:"data"`

	httpHeader
}

// ListModels Lists the currently available models,
// and provides basic information about each model such as the model id and parent.
func (c *Client) ListModels(ctx context.Context) (models ModelsList, err error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.fullURL("/models"))
	if err != nil {
		return
	}

	err = c.sendRequest(req, &models)
	return
}
