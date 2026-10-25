package httpapi

import (
	"time"

	"campusclaw/internal/retrieval"
	"campusclaw/internal/store"
	"campusclaw/internal/toolrun"
)

// Response envelopes referenced by the OpenAPI annotations. They mirror the
// JSON shapes the handlers actually return.

type errorBody struct {
	Error string `json:"error" example:"not found"`
}

type userBody struct {
	ID          int64  `json:"id" example:"1"`
	Username    string `json:"username" example:"teacher_a"`
	DisplayName string `json:"display_name" example:"教师 A"`
	Role        string `json:"role" example:"teacher"`
	ClassID     int64  `json:"class_id" example:"1"`
}

type loginResponse struct {
	Token string   `json:"token"`
	User  userBody `json:"user"`
}

type meResponse struct {
	User userBody `json:"user"`
}

type okResponse struct {
	Status string `json:"status" example:"ok"`
}

type materialListResponse struct {
	Materials []store.Material `json:"materials"`
	Total     int              `json:"total" example:"3"`
	Page      int              `json:"page" example:"1"`
	PageSize  int              `json:"page_size" example:"20"`
}

type materialDetailResponse struct {
	Material store.Material `json:"material"`
	BodyText string         `json:"body_text"`
}

type uploadResponse struct {
	MaterialID    int64  `json:"material_id" example:"1"`
	Title         string `json:"title" example:"A 班-数学自学笔记"`
	ChunkStrategy string `json:"chunk_strategy" example:"auto"`
	ChunkCount    int    `json:"chunk_count" example:"4"`
	Indexing      string `json:"indexing,omitempty"`
}

type reindexResponse struct {
	MaterialID int64 `json:"material_id" example:"1"`
	ChunkCount int   `json:"chunk_count" example:"4"`
}

type vectorPayload struct {
	ClassID          int64 `json:"class_id" example:"1"`
	MaterialID       int64 `json:"material_id" example:"1"`
	KnowledgeEntryID int64 `json:"knowledge_entry_id" example:"1"`
	ChunkID          int64 `json:"chunk_id" example:"1"`
	ChunkIndex       int   `json:"chunk_index" example:"0"`
}

type vectorChunkView struct {
	ChunkID          int64         `json:"chunk_id" example:"1"`
	KnowledgeEntryID int64         `json:"knowledge_entry_id" example:"1"`
	MaterialID       int64         `json:"material_id" example:"1"`
	MaterialTitle    string        `json:"material_title" example:"A 班-数学自学笔记"`
	ChunkIndex       int           `json:"chunk_index" example:"0"`
	StartOffset      int           `json:"start_offset" example:"0"`
	EndOffset        int           `json:"end_offset" example:"17"`
	IndexStatus      string        `json:"index_status" example:"ready"`
	EmbeddingModel   string        `json:"embedding_model"`
	Dimension        int           `json:"dimension" example:"2048"`
	Payload          vectorPayload `json:"payload"`
}

type vectorResponse struct {
	Chunks []vectorChunkView `json:"chunks"`
}

type askResponse struct {
	Answer    string     `json:"answer"`
	Status    string     `json:"status" example:"ok"`
	Citations []citation `json:"citations"`
}

type exampleListResponse struct {
	Examples []exampleQuestion `json:"examples"`
}

type agentListResponse struct {
	Agents []store.Agent `json:"agents"`
}

type skillListResponse struct {
	Skills []store.Skill `json:"skills"`
}

type conversationListResponse struct {
	Conversations []store.Conversation `json:"conversations"`
}

// messageView mirrors store.Message for OpenAPI without json.RawMessage, which
// swag cannot resolve.
type messageView struct {
	ID             int64                 `json:"id" example:"1"`
	ConversationID int64                 `json:"conversation_id" example:"1"`
	Role           string                `json:"role" example:"user"`
	Content        string                `json:"content" example:"移项怎么做"`
	Citations      []citation            `json:"citations"`
	SkillName      string                `json:"skill_name" example:"数学解题引导"`
	ToolCalls      []toolrun.CallSummary `json:"tool_calls"`
	CreatedAt      time.Time             `json:"created_at"`
}

type conversationDetailResponse struct {
	Conversation store.Conversation `json:"conversation"`
	Messages     []messageView      `json:"messages"`
}

// searchResponse aliases the retrieval result for annotation references.
type searchResponse = retrieval.Result
