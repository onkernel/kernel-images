package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/reclaimprotocol/reclaim-tee/client"

	"github.com/onkernel/kernel-images/server/cmd/api/circuits"
	"github.com/onkernel/kernel-images/server/lib/logger"
	oapi "github.com/onkernel/kernel-images/server/lib/oapi"
)

// Default TEE service URLs
const (
	defaultTEEKUrl     = "wss://tk.reclaimprotocol.org/ws"
	defaultTEETUrl     = "wss://tt.reclaimprotocol.org/ws"
	defaultAttestorUrl = "wss://attestor.reclaimprotocol.org:444/ws"
)

// reclaimConfigJSON is the structure for optional config overrides
type reclaimConfigJSON struct {
	TEEKUrl     string `json:"teekUrl,omitempty"`
	TEETUrl     string `json:"teetUrl,omitempty"`
	AttestorUrl string `json:"attestorUrl,omitempty"`
}

// ReclaimProve executes the TEE+MPC proof protocol
func (s *ApiService) ReclaimProve(ctx context.Context, req oapi.ReclaimProveRequestObject) (oapi.ReclaimProveResponseObject, error) {
	log := logger.FromContext(ctx)

	// Generate session ID
	sessionID := uuid.New()
	log.Info("starting reclaim prove", "session_id", sessionID.String())

	// Setup ZK callback (idempotent, only runs once)
	circuits.SetupZKCallback()

	// Determine TEE URLs: env vars > request config > defaults
	teekUrl := getEnvOrDefault("TEE_K_URL", defaultTEEKUrl)
	teetUrl := getEnvOrDefault("TEE_T_URL", defaultTEETUrl)
	attestorUrl := getEnvOrDefault("ATTESTOR_URL", defaultAttestorUrl)

	// Apply request-level config overrides if provided
	if req.Body.ConfigJson != nil && *req.Body.ConfigJson != "" {
		var cfg reclaimConfigJSON
		if err := json.Unmarshal([]byte(*req.Body.ConfigJson), &cfg); err == nil {
			if cfg.TEEKUrl != "" {
				teekUrl = cfg.TEEKUrl
			}
			if cfg.TEETUrl != "" {
				teetUrl = cfg.TEETUrl
			}
			if cfg.AttestorUrl != "" {
				attestorUrl = cfg.AttestorUrl
			}
		}
	}

	log.Info("using TEE configuration",
		"teek_url", teekUrl,
		"teet_url", teetUrl,
		"attestor_url", attestorUrl,
	)

	// Build config JSON for the client library
	clientConfigJSON, err := json.Marshal(reclaimConfigJSON{
		TEEKUrl:     teekUrl,
		TEETUrl:     teetUrl,
		AttestorUrl: attestorUrl,
	})
	if err != nil {
		log.Error("failed to marshal client config", "err", err)
		return oapi.ReclaimProve500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: "failed to prepare client configuration",
			},
		}, nil
	}

	// Create reclaim client from JSON
	reclaimClient, err := client.NewReclaimClientFromJSON(
		req.Body.ProviderParamsJson,
		string(clientConfigJSON),
	)
	if err != nil {
		log.Error("failed to create reclaim client", "err", err)
		return oapi.ReclaimProve400JSONResponse{
			BadRequestErrorJSONResponse: oapi.BadRequestErrorJSONResponse{
				Message: fmt.Sprintf("invalid provider parameters: %v", err),
			},
		}, nil
	}
	defer reclaimClient.Close()

	// Create a context with timeout (5 minutes for proof generation)
	proofCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Execute protocol in a goroutine so we can handle context cancellation
	type result struct {
		claim *client.ClaimWithSignatures
		err   error
	}
	resultCh := make(chan result, 1)

	go func() {
		claim, err := reclaimClient.ExecuteCompleteProtocol(nil)
		resultCh <- result{claim: claim, err: err}
	}()

	// Wait for result or context cancellation
	select {
	case <-proofCtx.Done():
		log.Error("proof execution timed out", "session_id", sessionID.String())
		return oapi.ReclaimProve500JSONResponse{
			InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
				Message: "proof execution timed out",
			},
		}, nil
	case res := <-resultCh:
		if res.err != nil {
			log.Error("proof execution failed", "session_id", sessionID.String(), "err", res.err)
			return oapi.ReclaimProve500JSONResponse{
				InternalErrorJSONResponse: oapi.InternalErrorJSONResponse{
					Message: fmt.Sprintf("proof execution failed: %v", res.err),
				},
			}, nil
		}

		log.Info("proof execution completed", "session_id", sessionID.String(), "identifier", res.claim.Claim.Identifier)

		// Map result to response
		return oapi.ReclaimProve200JSONResponse{
			SessionId: sessionID,
			Claim:     mapClaimToOapi(res.claim.Claim),
			Signature: mapSignatureToOapi(res.claim.Signature),
		}, nil
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func mapClaimToOapi(claim interface{}) oapi.ReclaimClaim {
	// The claim is a protobuf message, we need to extract fields
	// Using type assertion with the actual proto type
	type providerClaimData interface {
		GetProvider() string
		GetParameters() string
		GetOwner() string
		GetTimestampS() uint32
		GetContext() string
		GetIdentifier() string
		GetEpoch() uint32
	}

	if c, ok := claim.(providerClaimData); ok {
		provider := c.GetProvider()
		parameters := c.GetParameters()
		owner := c.GetOwner()
		timestampS := int(c.GetTimestampS())
		context := c.GetContext()
		identifier := c.GetIdentifier()
		epoch := int(c.GetEpoch())

		return oapi.ReclaimClaim{
			Provider:   &provider,
			Parameters: &parameters,
			Owner:      &owner,
			TimestampS: &timestampS,
			Context:    &context,
			Identifier: &identifier,
			Epoch:      &epoch,
		}
	}

	return oapi.ReclaimClaim{}
}

func mapSignatureToOapi(sig interface{}) oapi.ReclaimSignature {
	// The signature is a protobuf message
	type claimSignature interface {
		GetAttestorAddress() string
		GetClaimSignature() []byte
		GetResultSignature() []byte
	}

	if s, ok := sig.(claimSignature); ok {
		attestorAddr := s.GetAttestorAddress()
		claimSig := base64.StdEncoding.EncodeToString(s.GetClaimSignature())
		resultSig := base64.StdEncoding.EncodeToString(s.GetResultSignature())

		return oapi.ReclaimSignature{
			AttestorAddress: &attestorAddr,
			ClaimSignature:  &claimSig,
			ResultSignature: &resultSig,
		}
	}

	return oapi.ReclaimSignature{}
}
