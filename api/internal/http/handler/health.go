package handler

import (
	"context"
	"time"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
)

// DBPinger abstrai a verificação de conectividade com o banco de dados.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// Server implementa o contrato oapi.StrictServerInterface da API.
type Server struct {
	db DBPinger
}

// NewServer constrói o servidor HTTP com as dependências necessárias.
func NewServer(db DBPinger) *Server {
	return &Server{db: db}
}

// GetHealthz verifica a saúde da API e a conectividade com o PostgreSQL.
func (s *Server) GetHealthz(ctx context.Context, _ oapi.GetHealthzRequestObject) (oapi.GetHealthzResponseObject, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if s.db == nil {
		return oapi.GetHealthz503JSONResponse{
			Error: oapi.ErrorDetail{
				Code:    string(domain.CodeServiceUnavailable),
				Message: "Banco de dados não inicializado",
				Details: &map[string]any{"database": string(oapi.Down)},
			},
		}, nil
	}

	if err := s.db.Ping(pingCtx); err != nil {
		return oapi.GetHealthz503JSONResponse{
			Error: oapi.ErrorDetail{
				Code:    string(domain.CodeServiceUnavailable),
				Message: "Banco de dados indisponível",
				Details: &map[string]any{"database": string(oapi.Down)},
			},
		}, nil
	}

	return oapi.GetHealthz200JSONResponse{
		Status:    oapi.Ok,
		Database:  oapi.Up,
		Timestamp: time.Now().UTC(),
	}, nil
}
