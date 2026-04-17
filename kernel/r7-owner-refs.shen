\* r7-owner-refs.shen — Rule R7: owner-reference coherence

   Warn when Argo CD uses label tracking with Crossplane Compositions,
   since Crossplane's label propagation conflicts with Argo's tracking. *\

\* Check an Argo Application for label tracking + Composition conflict *\
(define check-r7-app
  {(list A) --> (list (list A)) --> (list judgment)}
  [argo-app-fact AppName TrackingMode _ AppSrc] Compositions ->
    (if (= TrackingMode "label")
        (flatten (map (/. Comp (check-r7-composition-conflict AppName AppSrc Comp))
                      Compositions))
        [])
  _ _ -> [])

\* Warn about a specific Composition when label tracking is active *\
(define check-r7-composition-conflict
  {string --> source-loc --> (list A) --> (list judgment)}
  AppName AppSrc [composition-fact CompName _ _ _ CompSrc] ->
    [(make-warning "XPC007"
      AppSrc
      (cn "Argo CD label tracking conflicts with Crossplane Composition " (cn CompName ""))
      (cn "Argo Application '" (cn AppName (cn "' uses label-based tracking, but Composition '"
        (cn CompName (cn "' produces resources that Crossplane will label-propagate to. "
          "This causes Argo CD to either prune Crossplane-created resources or fight Crossplane for ownership (see crossplane/crossplane#2121).")))))
      "Switch Argo CD tracking mode to annotation: set argocd.argoproj.io/tracking-method: annotation on the Application."
      [CompSrc])]
  _ _ _ -> [])

\* Top-level R7 check *\
(define check-r7
  {(list (list A)) --> (list (list A)) --> (list judgment)}
  ArgoApps Compositions ->
    (flatten (map (/. App (check-r7-app App Compositions)) ArgoApps)))
