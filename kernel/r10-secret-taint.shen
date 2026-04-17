\* r10-secret-taint.shen — Rule R10: secret taint leak (XPC010)

   For each resolved patch, if the source field is tainted (names a secret
   or credential-bearing field) and the destination field is NOT a
   secret sink (not a *SecretRef / not otherwise marked secret), emit
   XPC010. Mirrors pkg/obligation/secretflow/source_sink.go (now deleted).

   The kernel consumes resolved-patch facts produced by the Go bridge; each
   is shaped (resolved-patch CompName Src FromPath ToPath FromType ToType).
   The FromType/ToType fields are not used by R10 but are present in the
   tuple; we pattern-match on the full shape. *\


\* ===== Known-secret path tables ===== *\

\* Known-secret SOURCE field paths (exact-match list). *\
(define secret-source-field?
  {string --> boolean}
  "spec.writeConnectionSecretToRef" -> true
  "spec.publishConnectionDetailsTo" -> true
  "spec.connectionDetails" -> true
  "spec.credentials" -> true
  "spec.forProvider.credentials" -> true
  "spec.forProvider.password" -> true
  "spec.forProvider.secretKey" -> true
  "spec.forProvider.accessKey" -> true
  "spec.forProvider.masterPassword" -> true
  "spec.forProvider.masterUsername" -> true
  "spec.forProvider.adminPassword" -> true
  "spec.forProvider.token" -> true
  "spec.forProvider.apiKey" -> true
  "spec.forProvider.connectionString" -> true
  "spec.forProvider.privateKey" -> true
  "spec.forProvider.clientSecret" -> true
  "spec.forProvider.sslCertificate" -> true
  "spec.forProvider.sslKey" -> true
  "spec.forProvider.tlsCertificate" -> true
  "spec.forProvider.tlsKey" -> true
  _ -> false)

\* Known-secret SINK field paths (exact-match list). *\
(define secret-sink-field?
  {string --> boolean}
  "spec.writeConnectionSecretToRef" -> true
  "spec.publishConnectionDetailsTo" -> true
  "spec.connectionDetails" -> true
  "spec.forProvider.passwordSecretRef" -> true
  "spec.forProvider.credentials" -> true
  "spec.forProvider.credentialsSecretRef" -> true
  "spec.forProvider.masterPasswordSecretRef" -> true
  "spec.forProvider.tokenSecretRef" -> true
  "spec.forProvider.apiKeySecretRef" -> true
  "spec.forProvider.privateKeySecretRef" -> true
  "spec.forProvider.connectionStringSecretRef" -> true
  _ -> false)


\* ===== Suffix-pattern match (case-insensitive substring) =====
   The Go version lower-cases the path and looks for a fixed set of
   substrings. We replicate that with an explicit case-insensitive
   contains check, since shen-go's prelude does not expose a
   string-lower primitive. *\

(define lc-char
  {string --> string}
  "A" -> "a"  "B" -> "b"  "C" -> "c"  "D" -> "d"  "E" -> "e"
  "F" -> "f"  "G" -> "g"  "H" -> "h"  "I" -> "i"  "J" -> "j"
  "K" -> "k"  "L" -> "l"  "M" -> "m"  "N" -> "n"  "O" -> "o"
  "P" -> "p"  "Q" -> "q"  "R" -> "r"  "S" -> "s"  "T" -> "t"
  "U" -> "u"  "V" -> "v"  "W" -> "w"  "X" -> "x"  "Y" -> "y"
  "Z" -> "z"
  C -> C)

(define lc-chars
  {(list string) --> (list string)}
  [] -> []
  [C | Rest] -> [(lc-char C) | (lc-chars Rest)])

(define string-lower
  {string --> string}
  S -> (xpc-implode (lc-chars (explode S))))


\* Does the lower-cased path contain any of these substrings? *\
(define contains-any?
  {string --> (list string) --> boolean}
  _ [] -> false
  S [P | Rest] -> (if (string-contains? S P)
                      true
                      (contains-any? S Rest)))


\* Classify a patch source as secret-tainted. *\
(define tainted-source?
  {string --> boolean}
  Path -> (if (secret-source-field? Path)
              true
              (contains-any? (string-lower Path)
                ["password" "secret" "credential" "token" "apikey"
                 "privatekey" "accesskey" "secretkey" "connectionstring"])))


\* Classify a patch destination as a legitimate secret sink. *\
(define secret-sink?
  {string --> boolean}
  Path -> (if (secret-sink-field? Path)
              true
              (let Lower (string-lower Path)
                (if (string-contains? Lower "secretref")
                    true
                    (string-contains? Lower "secret")))))


\* ===== Per-patch check ===== *\

(define check-r10-patch
  {(list A) --> (list judgment)}
  [resolved-patch CompName Src FromPath ToPath FromType ToType] ->
    (if (and (tainted-source? FromPath)
             (not (secret-sink? ToPath)))
        [(make-error "XPC010"
          Src
          (cn "secret taint leak in Composition " CompName)
          (cn "Patch source " (cn FromPath
            (cn " is tainted (secret/credential material) but destination "
                (cn ToPath
                    " is not a secret sink. This may expose sensitive data in a non-secret field where it could be logged, displayed, or read by unprivileged controllers."))))
          (cn "Route the secret through a SecretRef field, or add annotation xpc.dev/declassify: "
              (cn FromPath " to acknowledge this is intentional."))
          [])]
        [])
  _ -> [])


\* Top-level R10 check. *\
(define check-r10
  {(list (list A)) --> (list judgment)}
  Patches -> (flatten (map (/. P (check-r10-patch P)) Patches)))
