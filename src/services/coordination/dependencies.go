package coordination

type TransferAdmission interface{ AcquireTransfer(string) (func(), error) }
