package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"

	corehttp "github.com/yunotools/eif/internal/core/protocol/httpclient"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	moduleutils "github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/utils"
)

func (c *hddtgdtClient) QueryInvoices(
	ctx context.Context,
	auth *AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	opts model.QueryOptions,
) (
	*model.InvoiceQueryResult,
	error,
) {
	path, err := buildQueryPath(
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

	size := opts.Size
	if size <= 0 {
		size = 15
	}

	if size > model.MaxInvoiceQuerySize {
		size = model.MaxInvoiceQuerySize
	}

	params := url.Values{}
	params.Set(
		"sort",
		"tdlap:desc",
	)
	params.Set(
		"size",
		strconv.Itoa(size),
	)
	params.Set(
		"search",
		search,
	)
	requestURL, err := corehttp.BuildURL(
		c.endpoint,
		path,
		params,
	)
	if err != nil {
		return nil, err
	}

	slog.Info(
		"HDDT GDT invoice request",
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

	var response model.InvoiceQueryResult
	err = httpClient.GetJSON(
		ctx,
		requestURL,
		c.buildAuthHeaders(auth.Token),
		&response,
	)
	updateAuthenticatedContext(httpClient, target, auth)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

func buildQueryPath(
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
) (
	string,
	error,
) {
	var prefix string

	switch channel {
	case model.InvoiceChannelStandard:
		prefix = "/api/query/invoices/"

	case model.InvoiceChannelSCO:
		prefix = "/api/sco-query/invoices/"

	default:
		return "", fmt.Errorf(
			"unsupported invoice channel %q",
			channel,
		)
	}

	switch direction {
	case model.InvoiceDirectionSold:
		return prefix + "sold", nil

	case model.InvoiceDirectionPurchase:
		return prefix + "purchase", nil

	default:
		return "", fmt.Errorf(
			"unsupported invoice direction %q",
			direction,
		)
	}
}
