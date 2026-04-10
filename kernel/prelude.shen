\* prelude.shen — shared datatypes and helper predicates for the xpc kernel *\

\* ===== Datatypes ===== *\

(datatype xpc-types

  \* --- Atoms --- *\

  X : string;
  ==================
  X : group;

  X : string;
  ==================
  X : kind;

  X : string;
  ==================
  X : version-name;

  X : string;
  ==================
  X : field-path;

  X : string;
  ==================
  X : schema-ref;

  X : string;
  ==================
  X : resource-name;

  X : string;
  ==================
  X : file-path;

  \* --- Compound types --- *\

  G : group; V : version-name; K : kind;
  ==================
  [gvk G V K] : gvk;

  \* --- Cost classes --- *\
  ==================
  none : cost-class;

  ==================
  identity : cost-class;

  ==================
  structural : cost-class;

  ==================
  webhook : cost-class;

  \* --- Severities --- *\
  ==================
  error : severity;

  ==================
  warn : severity;

  ==================
  info : severity;

  \* --- Source locations --- *\
  F : file-path; L : number;
  ==================
  [source F L] : source-loc;

  \* --- Schema field types --- *\
  ==================
  string-type : field-type;

  ==================
  integer-type : field-type;

  ==================
  number-type : field-type;

  ==================
  boolean-type : field-type;

  ==================
  object-type : field-type;

  ==================
  array-type : field-type;

  ==================
  unknown-type : field-type;)


\* ===== World database — asserted by the IR loader ===== *\

\* Each of these is a Prolog-style fact that gets asserted when
   the IR is loaded. The Shen kernel pattern-matches on them. *\

\* (crd-fact Group Kind Scope Versions Conversion SourceLoc)
   Versions = list of [VersionName Served Storage SchemaRef]
   Conversion = [Strategy CostClass WebhookService] *\

\* (xrd-fact Group Kind Scope APIVersion Versions SourceLoc)
   Versions = list of [VersionName Served Referenceable SchemaRef] *\

\* (composition-fact Name CompositeTypeRef Mode Pipeline SourceLoc)
   CompositeTypeRef = [gvk Group Version Kind]
   Pipeline = list of [StepName FunctionRef InputAPIVersion InputKind] *\

\* (function-fact Name Package InputVersions SourceLoc)
   InputVersions = list of strings *\

\* (resource-fact APIVersion Kind Name Namespace Annotations SourceLoc) *\

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

(define assoc
  {A --> (list (list A)) --> (list A)}
  _ [] -> []
  Key [[Key | Rest] | _] -> [Key | Rest]
  Key [_ | Xs] -> (assoc Key Xs))

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

\* Extract the version part from an apiVersion string "group/version" *\
(define api-version->version
  {string --> string}
  AV -> (let Parts (shen.split-string AV "/")
           (if (= (length Parts) 2)
               (hd (tl Parts))
               AV)))

\* Extract the group part from an apiVersion string "group/version" *\
(define api-version->group
  {string --> string}
  AV -> (let Parts (shen.split-string AV "/")
           (if (= (length Parts) 2)
               (hd Parts)
               "")))

\* Split a string by a delimiter character. Returns list of strings. *\
(define split-string
  {string --> string --> (list string)}
  S Delim -> (split-string-h (explode S) Delim [] []))

(define split-string-h
  {(list string) --> string --> (list string) --> (list string) --> (list string)}
  [] _ Acc Result -> (reverse [(implode (reverse Acc)) | Result])
  [D | Rest] D Acc Result -> (split-string-h Rest D [] [(implode (reverse Acc)) | Result])
  [C | Rest] D Acc Result -> (split-string-h Rest D [C | Acc] Result))

\* Reverse a list *\
(define reverse
  {(list A) --> (list A)}
  Xs -> (reverse-h Xs []))

(define reverse-h
  {(list A) --> (list A) --> (list A)}
  [] Acc -> Acc
  [X | Rest] Acc -> (reverse-h Rest [X | Acc]))

\* Implode a list of single-character strings into one string *\
(define implode
  {(list string) --> string}
  [] -> ""
  [S | Rest] -> (cn S (implode Rest)))

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
