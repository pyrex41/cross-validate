\* r15-appproject-whitelist.shen — Rule R15: AppProject kind whitelist (XPC.D.kind-whitelisted)

   Emit XPC.D.kind-whitelisted for every (Application, resource kind) pair
   where the kind is not present in the owning AppProject's whitelist.

   First-pass scope: direct-manifest Applications only (no Helm/Kustomize
   rendering). Every resource fact in scope is checked against the project
   whitelist of every ArgoApp that references that project.

   Wildcard semantics:
     - Group = "*" matches any group.
     - Kind  = "*" matches any kind. *\


\* group-kind-matches? — true if a whitelist entry covers (G K). *\
(define group-kind-matches?
  WGroup WKind G K ->
    (and (or (= WGroup "*") (= WGroup G))
         (or (= WKind  "*") (= WKind  K))))

\* r15-whitelisted? — true if (G, K) is covered by at least one entry in WL.
   Each entry is expected to be a (group-kind Group Kind) tuple. *\
(define r15-whitelisted?
  G K [] -> false
  G K [[group-kind WGroup WKind] | Rest] ->
    (if (group-kind-matches? WGroup WKind G K)
        true
        (r15-whitelisted? G K Rest))
  G K [_ | Rest] -> (r15-whitelisted? G K Rest))


\* r15-cluster-scoped? — true when (Group Kind) is a Cluster-scoped CRD.
   Falls back to false (namespace-scoped) when not found in CRDs. *\
(define r15-cluster-scoped?
  G K [] -> false
  G K [[crd-fact CG CK Scope | _] | Rest] ->
    (if (and (= G CG) (= K CK))
        (= Scope "Cluster")
        (r15-cluster-scoped? G K Rest))
  G K [_ | Rest] -> (r15-cluster-scoped? G K Rest))


\* r15-lookup-app-project — return the project name for AppName from
   argo-app-proj-link facts; returns "default" when not found. *\
(define r15-lookup-app-project
  AppName [] -> "default"
  AppName [[argo-app-proj-link AppName ProjName] | _] -> ProjName
  AppName [_ | Rest] -> (r15-lookup-app-project AppName Rest))


\* r15-find-appproject — return the argo-appproject fact for ProjName,
   or [] if not found. *\
(define r15-find-appproject
  ProjName [] -> []
  ProjName [[argo-appproject ProjName | Rest] | _] -> [argo-appproject ProjName | Rest]
  ProjName [_ | Rest] -> (r15-find-appproject ProjName Rest))


\* r15-owned-by? — true if Res's OwningApp matches AppName. Used to filter
   global Resources down to only those the current Application manages,
   so R15 no longer n×m-blames every (app, resource) pair. *\
(define r15-owned-by?
  AppName [resource-fact _ _ _ _ _ _ OwningApp] -> (= AppName OwningApp)
  _ _ -> false)


\* check-r15-resource — check one resource against one app's AppProject.
   Returns a list of judgments (empty when whitelisted). *\
(define check-r15-resource
  [resource-fact APIVersion Kind Name Namespace _ ResSrc _]
    AppName AppProject CRDs ->
      (if (= AppProject [])
          []
          (let Group     (api-version->group APIVersion)
               IsCluster (r15-cluster-scoped? Group Kind CRDs)
               ProjName  (hd (tl AppProject))
               CWL       (hd (tl (tl (tl AppProject))))
               NWL       (hd (tl (tl (tl (tl AppProject)))))
               WL        (if IsCluster CWL NWL)
               GDisplay  (if (= Group "") "core" Group)
            (if (r15-whitelisted? Group Kind WL)
                []
                [(make-error "XPC.D.kind-whitelisted"
                  ResSrc
                  (cn "kind " (cn Kind (cn " (group " (cn GDisplay (cn ") not in AppProject " (cn ProjName " whitelist"))))))
                  (cn "Application '" (cn AppName
                    (cn "' is managed by AppProject '" (cn ProjName
                      (cn "', but " (cn Kind
                        (cn " (group: " (cn GDisplay
                          (cn ") is not in the "
                            (cn (if IsCluster "clusterResourceWhitelist" "namespaceResourceWhitelist")
                              " of that AppProject. Argo CD will refuse to sync this resource."))))))))))
                  (cn "Add {group: " (cn GDisplay (cn ", kind: " (cn Kind "} to the whitelist in the AppProject."))))
                  [])])))
  _ _ _ _ -> [])


\* check-r15-app — check all resources against one app's AppProject.
   Filters Resources to only those owned by AppName before the check —
   unowned resources and those owned by other apps are ignored here. *\
(define check-r15-app
  [argo-app-fact AppName | _] AppProjLinks AppProjects Resources CRDs ->
    (let ProjName   (r15-lookup-app-project AppName AppProjLinks)
         AppProject (r15-find-appproject ProjName AppProjects)
         Owned      (filter (/. Res (r15-owned-by? AppName Res)) Resources)
      (flatten (map (/. Res
                      (check-r15-resource Res AppName AppProject CRDs))
                    Owned)))
  _ _ _ _ _ -> [])


\* r15-violation-to-judgment — Go precomputes app/resource/project joins and
   emits only resources that are not whitelisted. *\
(define r15-violation-to-judgment
  [r15-violation AppName ProjName Group Kind _ WLKey ResSrc] ->
    (let GDisplay (if (= Group "") "core" Group)
      (make-error "XPC.D.kind-whitelisted"
        ResSrc
        (cn "kind " (cn Kind (cn " (group " (cn GDisplay (cn ") not in AppProject " (cn ProjName " whitelist"))))))
        (cn "Application '" (cn AppName
          (cn "' is managed by AppProject '" (cn ProjName
            (cn "', but " (cn Kind
              (cn " (group: " (cn GDisplay
                (cn ") is not in the "
                  (cn WLKey " of that AppProject. Argo CD will refuse to sync this resource."))))))))))
        (cn "Add {group: " (cn GDisplay (cn ", kind: " (cn Kind "} to the whitelist in the AppProject."))))
        []))
  _ -> [])

\* check-r15 — top-level R15 check. *\
(define check-r15
  Violations -> (map (/. V (r15-violation-to-judgment V)) Violations))
