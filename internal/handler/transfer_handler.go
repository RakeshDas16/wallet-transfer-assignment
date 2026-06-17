package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/go-chi/chi/v5"
)

type TransferHandler struct {
	service *service.TransferService
	logger  *slog.Logger
}

func NewTransferHandler(svc *service.TransferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{service: svc, logger: logger}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var payload createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), domain.CreateTransferRequest{
		IdempotencyKey: payload.IdempotencyKey,
		FromWalletID:   payload.FromWalletID,
		ToWalletID:     payload.ToWalletID,
		Amount:         payload.Amount,
	})
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, result.HTTPStatus, result.Response)
}

func (h *TransferHandler) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")
	balance, err := h.service.GetWalletBalance(r.Context(), walletID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, balance)
}

func (h *TransferHandler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrMissingIdempotencyKey),
		errors.Is(err, domain.ErrMissingWalletID),
		errors.Is(err, domain.ErrSameWalletTransfer),
		errors.Is(err, domain.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		h.logger.Error("request failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
