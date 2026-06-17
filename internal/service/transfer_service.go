package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TransferService struct {
	repo repository.TransferRepository
}

func NewTransferService(repo repository.TransferRepository) *TransferService {
	return &TransferService{repo: repo}
}

type TransferResult struct {
	Response   domain.TransferResponse
	HTTPStatus int
	Replayed   bool
}

func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*TransferResult, error) {
	if err := domain.ValidateCreateTransferRequest(req); err != nil {
		return nil, err
	}

	requestHash := hashRequest(req)
	var result *TransferResult

	err := s.repo.WithTx(ctx, func(tx pgx.Tx) error {
		record, err := s.repo.ClaimIdempotency(ctx, tx, req.IdempotencyKey, requestHash)
		if err != nil {
			return err
		}

		if record.Status == domain.IdempotencyStatusCompleted {
			var response domain.TransferResponse
			if err := json.Unmarshal(record.ResponseBody, &response); err != nil {
				return fmt.Errorf("decode cached response: %w", err)
			}

			result = &TransferResult{
				Response:   response,
				HTTPStatus: *record.ResponseStatus,
				Replayed:   true,
			}
			return nil
		}

		transfer, err := s.repo.CreateTransfer(ctx, tx, domain.Transfer{
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.TransferStatusPending,
		})
		if err != nil {
			return err
		}

		fromWallet, _, err := s.repo.LockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
		if err != nil {
			return err
		}

		response := domain.TransferResponse{
			TransferID:   transfer.ID,
			Status:       transfer.Status,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
		}

		if fromWallet.Balance < req.Amount {
			reason := domain.ErrInsufficientFunds.Error()
			if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.TransferStatusFailed, &reason); err != nil {
				return err
			}

			response.Status = domain.TransferStatusFailed
			response.FailureReason = &reason
			return finishTransfer(ctx, s.repo, tx, req.IdempotencyKey, transfer.ID, response, 422, &result)
		}

		if err := s.repo.UpdateWalletBalance(ctx, tx, req.FromWalletID, -req.Amount); err != nil {
			return err
		}
		if err := s.repo.UpdateWalletBalance(ctx, tx, req.ToWalletID, req.Amount); err != nil {
			return err
		}

		if err := s.repo.CreateLedgerEntry(ctx, tx, domain.LedgerEntry{
			WalletID:   req.FromWalletID,
			TransferID: transfer.ID,
			EntryType:  domain.LedgerEntryTypeDebit,
			Amount:     req.Amount,
		}); err != nil {
			return err
		}

		if err := s.repo.CreateLedgerEntry(ctx, tx, domain.LedgerEntry{
			WalletID:   req.ToWalletID,
			TransferID: transfer.ID,
			EntryType:  domain.LedgerEntryTypeCredit,
			Amount:     req.Amount,
		}); err != nil {
			return err
		}

		if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.TransferStatusProcessed, nil); err != nil {
			return err
		}

		response.Status = domain.TransferStatusProcessed
		return finishTransfer(ctx, s.repo, tx, req.IdempotencyKey, transfer.ID, response, 201, &result)
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *TransferService) GetWalletBalance(ctx context.Context, walletID string) (*domain.WalletBalanceResponse, error) {
	if walletID == "" {
		return nil, domain.ErrMissingWalletID
	}

	wallet, err := s.repo.GetWalletBalance(ctx, walletID)
	if err != nil {
		return nil, err
	}

	return &domain.WalletBalanceResponse{
		WalletID: wallet.ID,
		Balance:  wallet.Balance,
	}, nil
}

func finishTransfer(
	ctx context.Context,
	repo repository.TransferRepository,
	tx pgx.Tx,
	idempotencyKey string,
	transferID uuid.UUID,
	response domain.TransferResponse,
	httpStatus int,
	result **TransferResult,
) error {
	body, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode transfer response: %w", err)
	}

	if err := repo.CompleteIdempotency(ctx, tx, idempotencyKey, transferID, httpStatus, body); err != nil {
		return err
	}

	*result = &TransferResult{
		Response:   response,
		HTTPStatus: httpStatus,
		Replayed:   false,
	}
	return nil
}

func hashRequest(req domain.CreateTransferRequest) string {
	payload := fmt.Sprintf("%s|%s|%d", req.FromWalletID, req.ToWalletID, req.Amount)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
