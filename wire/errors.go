// wire/errors.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import "errors"

var (
	// ErrConnectorNotFound is returned when an operation references a connector ID
	// that does not exist in the registry.
	//
	// Português: Retornado quando uma operação referencia um ID de conector que
	// não existe no registro.
	ErrConnectorNotFound = errors.New("wire: connector not found")

	// ErrConnectorLocked is returned when trying to connect to/from a locked connector.
	//
	// Português: Retornado quando se tenta conectar de/para um conector bloqueado.
	ErrConnectorLocked = errors.New("wire: connector is locked")

	// ErrIncompatibleTypes is returned when the output and input types are not compatible.
	//
	// Português: Retornado quando os tipos de saída e entrada não são compatíveis.
	ErrIncompatibleTypes = errors.New("wire: incompatible data types")

	// ErrAlreadyConnected is returned when a wire already exists between two connectors.
	//
	// Português: Retornado quando um fio já existe entre dois conectores.
	ErrAlreadyConnected = errors.New("wire: already connected")

	// ErrMaxConnectionsReached is returned when a connector has reached its maximum
	// number of connections.
	//
	// Português: Retornado quando um conector atingiu seu número máximo de conexões.
	ErrMaxConnectionsReached = errors.New("wire: maximum connections reached")

	// ErrSameElement is returned when trying to connect two ports on the same element.
	//
	// Português: Retornado quando se tenta conectar duas portas no mesmo elemento.
	ErrSameElement = errors.New("wire: cannot connect ports on the same element")

	// ErrSameDirection is returned when trying to connect two outputs or two inputs.
	//
	// Português: Retornado quando se tenta conectar duas saídas ou duas entradas.
	ErrSameDirection = errors.New("wire: cannot connect two outputs or two inputs")

	// ErrNoConnectionInProgress is returned when FinishConnect is called without
	// a preceding StartConnect.
	//
	// Português: Retornado quando FinishConnect é chamado sem um StartConnect anterior.
	ErrNoConnectionInProgress = errors.New("wire: no connection in progress")
)
