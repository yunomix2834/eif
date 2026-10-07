package module

import "github.com/gin-gonic/gin"

// Registrar router trung tâm không cần phải biết từng module
// Dependency inversion
type Registrar interface {
	RegisterRoutes(api *gin.RouterGroup)
}
