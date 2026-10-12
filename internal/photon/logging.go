package photon

import "os"

var logVerbose = os.Getenv("ERIDIAN_VERBOSE") == "1"

var noisyRPCs = map[string]bool{
	"RPC_ReportRaycastHit": true,
}

func suppressRPCLog(name string) bool { return !logVerbose && noisyRPCs[name] }

func isNoisyRaiseEvent(params map[byte]any) bool {
	if logVerbose {
		return false
	}
	switch asInt32(params[ParamEventCode]) {
	case 200, 201, 206:
		return true
	}
	return false
}
