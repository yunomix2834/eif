package handler

import "github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/service"

type Handler struct {
	service service.Service
}

func New(service service.Service) *Handler {
	return &Handler{service: service}
}
