package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	corehttp "github.com/yunomix2834/eif/internal/core/protocol/httpclient"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	moduleutils "github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/utils"
)

func (c *hddtgdtClient) ExportInvoices(
	ctx context.Context,
	auth *AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	opts model.ExportOptions,
) (
	*model.File,
	error,
) {
	path, extra, err := buildExportPath(
		channel,
		direction,
	)
	if err != nil {
		return nil, err
	}

	search, err := moduleutils.BuildSearch(
		opts.From,
		opts.To,
		opts.Filter,
	)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set(
		"sort",
		"tdlap:desc",
	)
	params.Set(
		"search",
		search,
	)
	for key, values := range extra {
		for _, value := range values {
			params.Add(
				key,
				value,
			)
		}
	}

	requestURL, err := corehttp.BuildURL(
		c.endpoint,
		path,
		params,
	)
	if err != nil {
		return nil, err
	}

	slog.Info(
		"HDDT GDT export request",
		"method",
		"GET",
		"url",
		requestURL,
		"params",
		params,
		"body",
		nil,
		"channel",
		channel,
		"direction",
		direction,
	)

	httpClient, target, err := c.authenticatedClient(auth)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.GetBytes(
		ctx,
		requestURL,
		c.buildAuthHeaders(auth.Token),
	)
	updateAuthenticatedContext(httpClient, target, auth)
	if err != nil {
		return nil, err
	}

	contentType := response.ContentType
	if contentType == "" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}

	return &model.File{
		Body:        response.Body,
		ContentType: contentType,
	}, nil
}

func buildExportPath(
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
) (
	string,
	url.Values,
	error,
) {
	extra := url.Values{}

	switch channel {
	case model.InvoiceChannelStandard:
		switch direction {
		case model.InvoiceDirectionSold:
			return "/api/query/invoices/export-excel", extra, nil

		case model.InvoiceDirectionPurchase:
			extra.Set(
				"type",
				"purchase",
			)
			return "/api/query/invoices/export-excel-sold", extra, nil
		}
	case model.InvoiceChannelSCO:
		switch direction {
		case model.InvoiceDirectionSold:
			return "/api/sco-query/invoices/export-excel", extra, nil

		case model.InvoiceDirectionPurchase:
			return "/api/sco-query/invoices/export-excel-sold", extra, nil
		}
	}
	return "", nil, fmt.Errorf(
		"unsupported export combination: channel=%q direction=%q",
		channel,
		direction,
	)
}
