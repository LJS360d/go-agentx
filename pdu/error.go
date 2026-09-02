// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "fmt"

// SNMPv2 error-status values. These are legal in a Response to
// TestSet/CommitSet/UndoSet (RFC 2741 6.2.6)
const (
	ErrorGenErr              Error = 5
	ErrorNoAccess            Error = 6
	ErrorWrongType           Error = 7
	ErrorWrongLength         Error = 8
	ErrorWrongEncoding       Error = 9
	ErrorWrongValue          Error = 10
	ErrorNoCreation          Error = 11
	ErrorInconsistentValue   Error = 12
	ErrorResourceUnavailable Error = 13
	ErrorCommitFailed        Error = 14
	ErrorUndoFailed          Error = 15
	ErrorNotWritable         Error = 17
	ErrorInconsistentName    Error = 18
)

// The various pdu packet errors.
const (
	ErrorNone                  Error = 0
	ErrorOpenFailed            Error = 256
	ErrorNotOpen               Error = 257
	ErrorIndexWrongType        Error = 258
	ErrorIndexAlreadyAllocated Error = 259
	ErrorIndexNoneAvailable    Error = 260
	ErrorIndexNotAllocated     Error = 261
	ErrorUnsupportedContext    Error = 262
	ErrorDuplicateRegistration Error = 263
	ErrorUnknownRegistration   Error = 264
	ErrorUnknownAgentCaps      Error = 265
	ErrorParse                 Error = 266
	ErrorRequestDenied         Error = 267
	ErrorProcessing            Error = 268
)

// Error defines a pdu packet error.
type Error uint16

// Error lets an Error be returned directly from a Handler, so session.handle
// can recover the exact code with errors.As.
func (e Error) Error() string {
	return e.String()
}

func (e Error) String() string {
	switch e {
	case ErrorNone:
		return "ErrorNone"
	case ErrorGenErr:
		return "ErrorGenErr"
	case ErrorNoAccess:
		return "ErrorNoAccess"
	case ErrorWrongType:
		return "ErrorWrongType"
	case ErrorWrongLength:
		return "ErrorWrongLength"
	case ErrorWrongEncoding:
		return "ErrorWrongEncoding"
	case ErrorWrongValue:
		return "ErrorWrongValue"
	case ErrorNoCreation:
		return "ErrorNoCreation"
	case ErrorInconsistentValue:
		return "ErrorInconsistentValue"
	case ErrorResourceUnavailable:
		return "ErrorResourceUnavailable"
	case ErrorCommitFailed:
		return "ErrorCommitFailed"
	case ErrorUndoFailed:
		return "ErrorUndoFailed"
	case ErrorNotWritable:
		return "ErrorNotWritable"
	case ErrorInconsistentName:
		return "ErrorInconsistentName"
	case ErrorOpenFailed:
		return "ErrorOpenFailed"
	case ErrorNotOpen:
		return "ErrorNotOpen"
	case ErrorIndexWrongType:
		return "ErrorIndexWrongType"
	case ErrorIndexAlreadyAllocated:
		return "ErrorIndexAlreadyAllocated"
	case ErrorIndexNoneAvailable:
		return "ErrorIndexNoneAvailable"
	case ErrorIndexNotAllocated:
		return "ErrorIndexNotAllocated"
	case ErrorUnsupportedContext:
		return "ErrorUnsupportedContext"
	case ErrorDuplicateRegistration:
		return "ErrorDuplicateRegistration"
	case ErrorUnknownRegistration:
		return "ErrorUnknownRegistration"
	case ErrorUnknownAgentCaps:
		return "ErrorUnknownAgentCaps"
	case ErrorParse:
		return "ErrorParse"
	case ErrorRequestDenied:
		return "ErrorRequestDenied"
	case ErrorProcessing:
		return "ErrorProcessing"
	}
	return fmt.Sprintf("ErrorUnknown (%d)", e)
}
