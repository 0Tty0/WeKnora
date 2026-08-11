package service

import (
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/artifact"
	"github.com/Tencent/WeKnora/internal/types"
)

const chunkEmbeddingFingerprintMetadataKey = "_weknora_embedding_artifact_key"

type chunkReusePlan struct {
	Index        []*types.Chunk
	Reuse        []*types.Chunk
	Fingerprints map[string]string
}

type chunkFingerprintFunc func(*types.Chunk) (fingerprint string, available bool, err error)

// chunkVectorFingerprint binds reusable vector content to its publication
// target. Bump the version if index-input normalization semantics change.
func chunkVectorFingerprint(
	embeddingKey string,
	vectorStoreID *string,
	knowledgeType string,
	strategy types.IndexingStrategy,
) (string, error) {
	storeID := ""
	if vectorStoreID != nil {
		storeID = *vectorStoreID
	}
	canonical, err := artifact.CanonicalJSON(struct {
		Version        string `json:"version"`
		EmbeddingKey   string `json:"embedding_key"`
		VectorStoreID  string `json:"vector_store_id"`
		KnowledgeType  string `json:"knowledge_type"`
		VectorEnabled  bool   `json:"vector_enabled"`
		KeywordEnabled bool   `json:"keyword_enabled"`
	}{
		Version:        "chunk-vector:v1",
		EmbeddingKey:   embeddingKey,
		VectorStoreID:  storeID,
		KnowledgeType:  knowledgeType,
		VectorEnabled:  strategy.VectorEnabled,
		KeywordEnabled: strategy.KeywordEnabled,
	})
	if err != nil {
		return "", err
	}
	return artifact.SHA256Hex(canonical), nil
}

// planChunkReuse separates text chunks that need a vector upsert from chunks
// whose published vector matches the current effective embedding request.
func planChunkReuse(
	desired []*types.Chunk,
	existing []*types.Chunk,
	indexingEnabled bool,
	fingerprint chunkFingerprintFunc,
) (*chunkReusePlan, error) {
	plan := &chunkReusePlan{Fingerprints: make(map[string]string)}
	if !indexingEnabled {
		return plan, nil
	}

	existingByID := make(map[string]*types.Chunk, len(existing))
	for _, chunk := range existing {
		if chunk != nil {
			existingByID[chunk.ID] = chunk
		}
	}

	for _, chunk := range desired {
		if chunk == nil || chunk.ChunkType != types.ChunkTypeText {
			continue
		}
		if fingerprint == nil {
			plan.Index = append(plan.Index, chunk)
			continue
		}

		desiredFingerprint, available, err := fingerprint(chunk)
		if err != nil {
			return nil, fmt.Errorf("fingerprint chunk %q: %w", chunk.ID, err)
		}
		if !available || desiredFingerprint == "" {
			plan.Index = append(plan.Index, chunk)
			continue
		}
		plan.Fingerprints[chunk.ID] = desiredFingerprint

		live := existingByID[chunk.ID]
		if live != nil &&
			live.Status == int(types.ChunkStatusIndexed) &&
			chunkEmbeddingFingerprint(live.Metadata) == desiredFingerprint {
			plan.Reuse = append(plan.Reuse, chunk)
			continue
		}
		plan.Index = append(plan.Index, chunk)
	}
	return plan, nil
}

func markChunkEmbeddingsIndexed(chunks []*types.Chunk, fingerprints map[string]string) error {
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		chunk.Status = int(types.ChunkStatusIndexed)
		fingerprint := fingerprints[chunk.ID]
		if fingerprint == "" {
			continue
		}
		metadata, err := withChunkEmbeddingFingerprint(chunk.Metadata, fingerprint)
		if err != nil {
			return fmt.Errorf("record embedding fingerprint for chunk %q: %w", chunk.ID, err)
		}
		chunk.Metadata = metadata
	}
	return nil
}

func chunkEmbeddingFingerprint(metadata types.JSON) string {
	values := make(map[string]json.RawMessage)
	if len(metadata) == 0 || json.Unmarshal(metadata, &values) != nil {
		return ""
	}
	var fingerprint string
	_ = json.Unmarshal(values[chunkEmbeddingFingerprintMetadataKey], &fingerprint)
	return fingerprint
}

func withChunkEmbeddingFingerprint(metadata types.JSON, fingerprint string) (types.JSON, error) {
	values := make(map[string]json.RawMessage)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &values); err != nil {
			return nil, err
		}
	}
	if values == nil {
		values = make(map[string]json.RawMessage)
	}
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return nil, err
	}
	values[chunkEmbeddingFingerprintMetadataKey] = encoded
	result, err := json.Marshal(values)
	return types.JSON(result), err
}
