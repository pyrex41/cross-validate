\* prelude.shen — shared datatypes and helper predicates for the xpc kernel *\


\* ===== World database — asserted by the IR loader ===== *\

\* Each of these is a Prolog-style fact that gets asserted when
   the IR is loaded. The Shen kernel pattern-matches on them. *\

\* (crd-fact Group Kind Scope Versions Conversion SourceLoc)
   Versions = list of [VersionName Served Storage SchemaRef]
   Conversion = [Strategy CostClass WebhookService] *\

\* (xrd-fact Group Kind Scope APIVersion Versions SourceLoc OwningApp)
   Versions = list of [VersionName Served Referenceable SchemaRef] *\

\* (composition-fact Name CompositeTypeRef Mode Pipeline SourceLoc OwningApp)
   CompositeTypeRef = [gvk Group Version Kind]
   Pipeline = list of [StepName FunctionRef InputAPIVersion InputKind] *\

\* (function-fact Name Package InputVersions SourceLoc OwningApp)
   InputVersions = list of strings *\

\* (provider-fact Name Package SourceLoc OwningApp) *\

\* (resource-fact APIVersion Kind Name Namespace Annotations SourceLoc OwningApp)
   OwningApp = name of the ArgoApplication that manages this resource (or
   CRD/XRD/Composition/Function/Provider), or "" for unowned (shared/global)
   facts. Per-app rules (R6a/b/c/d, R15) filter on this to avoid n×m
   cartesian emissions. *\

\* (argo-app-fact Name TrackingMode SyncWaves SourceLoc)
   SyncWaves = list of [Kind Name Wave] *\

\* (schema-fact Digest FieldMap)
   FieldMap = list of [DottedPath FieldType] *\


\* ===== Helper functions ===== *\

(define member
  {A --> (list A) --> boolean}
  _ [] -> false
  X [X | _] -> true
  X [_ | Rest] -> (member X Rest))

(define find-first
  {(A --> boolean) --> (list A) --> (list A)}
  _ [] -> []
  F [X | Rest] -> [X] where (F X)
  F [_ | Rest] -> (find-first F Rest))

(define filter
  {(A --> boolean) --> (list A) --> (list A)}
  _ [] -> []
  F [X | Rest] -> [X | (filter F Rest)] where (F X)
  F [_ | Rest] -> (filter F Rest))

(define count-if
  {(A --> boolean) --> (list A) --> number}
  _ [] -> 0
  F [X | Rest] -> (+ 1 (count-if F Rest)) where (F X)
  F [_ | Rest] -> (count-if F Rest))

(define lookup
  {string --> (list (list string)) --> string}
  _ [] -> ""
  Key [[Key Val] | _] -> Val
  Key [_ | Rest] -> (lookup Key Rest))

(define xpc-assoc
  {A --> (list (list A)) --> (list A)}
  _ [] -> []
  Key [[Key | Rest] | _] -> [Key | Rest]
  Key [_ | Xs] -> (xpc-assoc Key Xs))

(define flatten
  {(list (list A)) --> (list A)}
  [] -> []
  [X | Rest] -> (append X (flatten Rest)))

\* Make a judgment *\
(define make-judgment
  {string --> severity --> source-loc --> string --> string --> string --> (list source-loc) --> judgment}
  Code Sev Src Msg Detail Fix Related ->
    [judgment Code Sev Src Msg Detail Fix Related])

(define make-error
  {string --> source-loc --> string --> string --> string --> (list source-loc) --> judgment}
  Code Src Msg Detail Fix Related ->
    (make-judgment Code error Src Msg Detail Fix Related))

(define make-warning
  {string --> source-loc --> string --> string --> string --> (list source-loc) --> judgment}
  Code Src Msg Detail Fix Related ->
    (make-judgment Code warn Src Msg Detail Fix Related))

\* make-satisfied emits a marker judgment for a rule that ran and found no
   violations. The bridge filters these out of the visible diagnostics list
   but counts them in RunResult.Satisfied / TotalObligations. *\
(define make-satisfied
  {string --> judgment}
  Code -> [judgment Code satisfied [source "" 0] "" "" "" []])

\* mark-rule — if a rule produced no violations, emit a single satisfied
   marker judgment. Otherwise pass the violations through untouched. *\
(define mark-rule
  {string --> (list judgment) --> (list judgment)}
  Code [] -> [(make-satisfied Code)]
  _ Js -> Js)

\* Extract the version part from an apiVersion string "group/version" *\
(define api-version->version
  {string --> string}
  AV -> (let Parts (split-string AV "/")
           (if (= (length Parts) 2)
               (hd (tl Parts))
               AV)))

\* Extract the group part from an apiVersion string "group/version" *\
(define api-version->group
  {string --> string}
  AV -> (let Parts (split-string AV "/")
           (if (= (length Parts) 2)
               (hd Parts)
               "")))

\* Split a string by a delimiter character. Returns list of strings. *\
(define split-string
  {string --> string --> (list string)}
  S Delim -> (split-string-h (explode S) Delim [] []))

(define split-string-h
  {(list string) --> string --> (list string) --> (list string) --> (list string)}
  [] _ Acc Result -> (xpc-reverse [(xpc-implode (xpc-reverse Acc)) | Result])
  [D | Rest] D Acc Result -> (split-string-h Rest D [] [(xpc-implode (xpc-reverse Acc)) | Result])
  [C | Rest] D Acc Result -> (split-string-h Rest D [C | Acc] Result))

\* Reverse a list *\
(define xpc-reverse
  {(list A) --> (list A)}
  Xs -> (xpc-reverse-h Xs []))

(define xpc-reverse-h
  {(list A) --> (list A) --> (list A)}
  [] Acc -> Acc
  [X | Rest] Acc -> (xpc-reverse-h Rest [X | Acc]))

\* Implode a list of single-character strings into one string *\
(define xpc-implode
  {(list string) --> string}
  [] -> ""
  [S | Rest] -> (cn S (xpc-implode Rest)))

\* Explode a string into a list of single-character strings *\
\* Uses Shen's built-in (explode S) which is actually (str->list) *\

(define string-contains?
  {string --> string --> boolean}
  Haystack Needle -> (string-contains-h? (explode Haystack) (explode Needle)))

(define string-contains-h?
  {(list string) --> (list string) --> boolean}
  _ [] -> true
  [] _ -> false
  Hs Ns -> (if (starts-with? Hs Ns)
               true
               (string-contains-h? (tl Hs) Ns)))

(define starts-with?
  {(list string) --> (list string) --> boolean}
  _ [] -> true
  [] _ -> false
  [H | Hs] [H | Ns] -> (starts-with? Hs Ns)
  _ _ -> false)

\* Integer less-than as a function *\
(define less-than?
  {number --> number --> boolean}
  X Y -> (< X Y))


\* ===== Trajectory-section accessors =====
   Every trajectory-facing rule (R12/R13/R14) reads resource keys out of a
   (delta (created ...) (updated ...) (deleted ...)) form and a (state ...)
   section. Keep the accessors here so load-order between rule files does
   not matter. *\

(define delta-created-keys
  {(list A) --> (list (list A))}
  [delta [created | Keys] _ _] -> Keys
  _ -> [])

(define delta-updated-keys
  {(list A) --> (list (list A))}
  [delta _ [updated | Keys] _] -> Keys
  _ -> [])

(define delta-deleted-keys
  {(list A) --> (list (list A))}
  [delta _ _ [deleted | Keys]] -> Keys
  _ -> [])

(define state-keys
  {(list A) --> (list (list A))}
  [state | Keys] -> Keys
  _ -> [])

\* key-in? — does (resource-key _ Kind Ns Name) appear in the list of keys? *\
(define key-in?
  {string --> string --> string --> (list (list A)) --> boolean}
  _ _ _ [] -> false
  Kind Name Ns [[resource-key _ Kind Ns Name] | _] -> true
  Kind Name Ns [_ | Rest] -> (key-in? Kind Name Ns Rest))

\* tail-steps — given a reference (AppName, Wave) and the full trajectory
   step list, return the sublist of later steps (strictly greater Wave)
   that belong to the same AppName. Used by cross-step rules that need
   to reason about later trajectory states for the same Argo Application. *\
(define tail-steps
  {string --> number --> (list (list A)) --> (list (list A))}
  _ _ [] -> []
  App W [[step App W2 D St] | Rest] -> [[step App W2 D St] | (tail-steps App W Rest)] where (> W2 W)
  App W [_ | Rest] -> (tail-steps App W Rest))
