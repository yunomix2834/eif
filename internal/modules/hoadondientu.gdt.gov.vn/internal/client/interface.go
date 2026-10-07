package client

import (
	"context"

	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/session"
)

type AuthenticatedContext struct {
	Token   string
	Cookies []session.Cookie
}

type AuthenticationResult struct {
	Token   string
	Cookies []session.Cookie
}

type Client interface {
	GetCaptcha(
		ctx context.Context,
	) (
		*dto.CaptchaResponse,
		error,
	)

	Authenticate(
		ctx context.Context,
		req *dto.AuthenticationRequest,
	) (
		*AuthenticationResult,
		error,
	)

	QueryInvoices(
		ctx context.Context,
		auth *AuthenticatedContext,
		channel model.InvoiceChannel,
		direction model.InvoiceDirection,
		opts model.QueryOptions,
	) (
		*model.InvoiceQueryResult,
		error,
	)

	ExportInvoices(
		ctx context.Context,
		auth *AuthenticatedContext,
		channel model.InvoiceChannel,
		direction model.InvoiceDirection,
		opts model.ExportOptions,
	) (
		*model.File,
		error,
	)
}
