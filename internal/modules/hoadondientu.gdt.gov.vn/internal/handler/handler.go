package handler

import "github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/service"

type Handler struct {
	service service.Service
}

func New(service service.Service) *Handler {
	return &Handler{service: service}
}
