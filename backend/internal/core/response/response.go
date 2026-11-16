package response

import "github.com/gin-gonic/gin"

type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func Success(c *gin.Context, data any) {
	c.JSON(200, Envelope{Code: 0, Message: "success", Data: data})
}
func Error(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, Envelope{Code: code, Message: message, Data: nil})
}

type Kind int

const (
	BadRequest Kind = iota
	Unauthenticated
	Forbidden
	NotFound
	Conflict
	Unprocessable
	TooManyRequests
	Internal
	Unavailable
	CSRFInvalid
	PayloadTooLarge
	UnsupportedMedia
	RangeNotSatisfiable
)

// Only fixed, public messages cross the API boundary; never serialize internal errors.
func Reject(c *gin.Context, kind Kind) {
	type definition struct {
		status, code int
		message      string
	}
	definitions := map[Kind]definition{
		BadRequest: {400, 40000, "Invalid request"}, Unauthenticated: {401, 40100, "Authentication required"},
		Forbidden: {403, 40300, "Permission denied"}, NotFound: {404, 40400, "Resource not found"},
		Conflict: {409, 40900, "Resource conflict"}, Unprocessable: {422, 42200, "Validation failed"},
		TooManyRequests: {429, 42900, "Too many requests"}, Internal: {500, 50000, "Internal server error"},
		Unavailable: {503, 50301, "Service not ready"}, CSRFInvalid: {403, 40301, "CSRF validation failed"},
		PayloadTooLarge: {413, 41300, "File too large"}, UnsupportedMedia: {415, 41500, "Unsupported media type"},
		RangeNotSatisfiable: {416, 41600, "Range requests are not supported"},
	}
	d, ok := definitions[kind]
	if !ok {
		d = definitions[Internal]
	}
	Error(c, d.status, d.code, d.message)
}
