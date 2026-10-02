package esim

import (
	"errors"
	"fmt"

	sgp22 "github.com/damonto/euicc-go/v2"
)

const (
	DownloadErrorGeneric                   = "download_failed"
	DownloadErrorEUICCInsufficientMemory   = "euicc_insufficient_memory"
	DownloadErrorEUICCIccidAlreadyExists   = "euicc_iccid_already_exists"
	DownloadErrorEUICCPPRNotAllowed        = "euicc_ppr_not_allowed"
	DownloadErrorEUICCProfileDataMismatch  = "euicc_profile_data_mismatch"
	DownloadErrorEUICCProfileInterrupted   = "euicc_profile_install_interrupted"
	DownloadErrorEUICCProfileInstallFailed = "euicc_profile_install_failed"
)

type DownloadErrorInfo struct {
	Code            string
	Message         string
	Details         string
	BPPCommandID    byte
	BPPErrorReason  byte
	OriginalMessage string
}

type DownloadProfileError struct {
	DownloadErrorInfo
	Err error
}

func NewDownloadProfileError(err error) *DownloadProfileError {
	info := ClassifyDownloadError(err)
	return &DownloadProfileError{
		DownloadErrorInfo: info,
		Err:               err,
	}
}

func (e *DownloadProfileError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "下載 profile 失敗"
}

func (e *DownloadProfileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ClassifyDownloadError(err error) DownloadErrorInfo {
	if err == nil {
		return DownloadErrorInfo{
			Code:    DownloadErrorGeneric,
			Message: "下載 profile 失敗",
		}
	}

	info := DownloadErrorInfo{
		Code:            DownloadErrorGeneric,
		Message:         fmt.Sprintf("下載 profile 失敗: %v", err),
		OriginalMessage: err.Error(),
	}

	var bppPtr *sgp22.LoadBoundProfilePackageError
	var bppValue sgp22.LoadBoundProfilePackageError
	if errors.As(err, &bppPtr) && bppPtr != nil {
		info = applyBPPDownloadErrorInfo(info, *bppPtr)
	} else if errors.As(err, &bppValue) {
		info = applyBPPDownloadErrorInfo(info, bppValue)
	}

	return info
}

func applyBPPDownloadErrorInfo(info DownloadErrorInfo, err sgp22.LoadBoundProfilePackageError) DownloadErrorInfo {
	info.BPPCommandID = byte(err.BPPCommandID)
	info.BPPErrorReason = byte(err.ErrorReason)
	info.Details = err.Error()
	info.Code = classifyBPPErrorCode(err)
	info.Message = downloadBPPErrorMessage(err)
	return info
}

func classifyBPPErrorCode(err sgp22.LoadBoundProfilePackageError) string {
	if err.BPPCommandID != sgp22.BPPCommandIDLoadProfileElements {
		return DownloadErrorEUICCProfileInstallFailed
	}
	switch err.ErrorReason {
	case sgp22.BPPErrorReasonInstallFailedDueToICCIDAlreadyExistsOnEUICC:
		return DownloadErrorEUICCIccidAlreadyExists
	case sgp22.BPPErrorReasonInstallFailedDueToInsufficientMemoryForProfile:
		return DownloadErrorEUICCInsufficientMemory
	case sgp22.BPPErrorReasonInstallFailedDueToInterruption:
		return DownloadErrorEUICCProfileInterrupted
	case sgp22.BPPErrorReasonInstallFailedDueToDataMismatch:
		return DownloadErrorEUICCProfileDataMismatch
	case sgp22.BPPErrorReasonPPRNotAllowed:
		return DownloadErrorEUICCPPRNotAllowed
	default:
		return DownloadErrorEUICCProfileInstallFailed
	}
}

func downloadBPPErrorMessage(err sgp22.LoadBoundProfilePackageError) string {
	if err.BPPCommandID != sgp22.BPPCommandIDLoadProfileElements {
		return fmt.Sprintf("eUICC 安裝 profile 失敗（%s）", err.Error())
	}
	switch err.ErrorReason {
	case sgp22.BPPErrorReasonInstallFailedDueToICCIDAlreadyExistsOnEUICC:
		return "eUICC 已存在相同 ICCID 的 profile"
	case sgp22.BPPErrorReasonInstallFailedDueToInsufficientMemoryForProfile:
		return "eUICC 安裝 profile 時空間不足，請刪除未使用的 profile 後重試"
	case sgp22.BPPErrorReasonInstallFailedDueToInterruption:
		return "eUICC 安裝 profile 時被中斷，請稍後重試"
	case sgp22.BPPErrorReasonInstallFailedDueToDataMismatch:
		return "eUICC 安裝 profile 時資料驗證不符"
	case sgp22.BPPErrorReasonPPRNotAllowed:
		return "eUICC 策略規則不允許安裝該 profile"
	default:
		return fmt.Sprintf("eUICC 安裝 profile 失敗（%s）", err.Error())
	}
}
