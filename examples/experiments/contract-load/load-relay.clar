(define-public (write2 (key uint) (payload (buff 4096)))
 (contract-call? .load-store write2 key payload))
(define-read-only (read2 (key uint))
 (contract-call? .load-store read2 key))

(define-public (write8 (key uint) (payload (buff 4096)))
 (contract-call? .load-store write8 key payload))
(define-read-only (read8 (key uint))
 (contract-call? .load-store read8 key))

(define-public (write16 (key uint) (payload (buff 4096)))
 (contract-call? .load-store write16 key payload))
(define-read-only (read16 (key uint))
 (contract-call? .load-store read16 key))
