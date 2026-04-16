\* r10-secret-taint.shen — Rule R10: secret-taint propagation

   Information-flow typing for connection-detail material.
   Marks fields as secret at the schema layer, propagates the taint through
   patches, and errors if a tainted value reaches an untainted sink. *\

\* Known secret source fields — field paths that are inherently secret. *\
(define is-known-secret-source?
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

\* Known secret sink fields — destinations that properly receive secrets. *\
(define is-known-secret-sink?
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

\* Heuristic patterns checked as substrings (case-insensitive via string-downcase). *\
(define secret-heuristic-patterns
  {A --> (list string)}
  _ -> ["password" "secret" "credential" "token" "apikey"
        "privatekey" "accesskey" "secretkey" "connectionstring"])

\* Is a field path tainted (secret source)? *\
(define is-secret-field?
  {string --> boolean}
  Path -> (if (is-known-secret-source? Path)
              true
              (let Lower (string-downcase Path)
                (any-substring? Lower (secret-heuristic-patterns [])))))

\* Is a field path a proper secret sink? *\
(define is-secret-sink?
  {string --> boolean}
  Path -> (if (is-known-secret-sink? Path)
              true
              (let Lower (string-downcase Path)
                (or (string-contains? Lower "secretref")
                    (string-contains? Lower "secret")))))

\* Check if any of the patterns appears as a substring *\
(define any-substring?
  {string --> (list string) --> boolean}
  _ [] -> false
  S [P | Rest] -> (if (string-contains? S P)
                      true
                      (any-substring? S Rest)))

\* Check a single patch for secret taint leak *\
(define check-r10-patch
  {string --> source-loc --> (list A) --> (list judgment)}
  CompName CompSrc [patch _ FromPath ToPath | _] ->
    (if (= FromPath "")
        []
        (if (= ToPath "")
            []
            (if (and (is-secret-field? FromPath) (not (is-secret-sink? ToPath)))
                [(make-error "XPC010"
                  CompSrc
                  (cn "secret taint leak in Composition " (cn CompName ""))
                  (cn "Patch source " (cn FromPath
                    (cn " is tainted (secret/credential material) but destination " (cn ToPath
                      " is not a secret sink. This may expose sensitive data in a non-secret field where it could be logged, displayed, or read by unprivileged controllers."))))
                  (cn "Route the secret through a SecretRef field, or add annotation xpc.dev/declassify: \""
                    (cn FromPath "\" to acknowledge this is intentional."))
                  [])]
                [])))
  _ _ _ -> [])

\* Check all patches in a composed resource *\
(define check-r10-composed-resource
  {string --> source-loc --> (list A) --> (list judgment)}
  CompName CompSrc [composed-resource _ _ _ Patches] ->
    (flatten (map (/. P (check-r10-patch CompName CompSrc P)) Patches))
  _ _ _ -> [])

\* Check a single Composition for secret taint leaks *\
(define check-r10-composition
  {(list A) --> (list judgment)}
  [composition-fact CompName _ _ _ Resources CompSrc] ->
    (flatten (map (/. R (check-r10-composed-resource CompName CompSrc R)) Resources))
  _ -> [])

\* Top-level R10 check *\
(define check-r10
  {(list (list A)) --> (list judgment)}
  Compositions ->
    (flatten (map (/. C (check-r10-composition C)) Compositions)))
