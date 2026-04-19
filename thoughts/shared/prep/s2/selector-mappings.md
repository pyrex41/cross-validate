# S2 selector → resolved-path registry (prep artifact)
Source: fg-manifold MR history + provider CRDs
Destination: pkg/ir/selector_registry.go in S2

## Notes on grounding

Rows are anchored in one of:

- **fg-manifold commit** where an `ignoreDifferences` / selector fix was merged. MRs cited in the base plan (`!1344`, `!1341`, `!1250`, `!883`, `!890`, `!1366`, `!36`) did not carry MR numbers in the public `git log` of `~/fg/fg-manifold` (GitLab default-merge messages), so each row instead cites the fg-manifold commit SHA whose diff is the authoritative fix. Commit SHAs map 1:1 to the MRs listed in the base plan by subject line and date — they are the same fixes, just addressed by SHA rather than GitLab MR number. The marker `CRD-doc` is used when the row comes from reading the live manifest / provider convention rather than a drift-suppression commit.
- **Upbound provider naming convention**: every `*IdSelector` resolves to `*Id`, every `*NameSelector` resolves to `*Name`, every `*ArnSelector` resolves to `*Arn`, and every bare-form `subnetSelector` / `securityGroupSelector` (used inside composition-level XRD specs like `FargateService`) resolves to the correspondingly named plural (`subnetIds` / `securityGroupIds`) or singular ARN — these are embedded XRD-composition shapes, not Upbound CRD fields directly. The registry treats both the resolved-primitive path (e.g. `subnetIds`) and the sibling `*Refs` path as needing `ignoreDifferences`, because fg-manifold has had to suppress both in practice (see commits `3ea4b28d0`, `7ac5a5f14`).

## Rows (53 entries; 23 anchored to fg-manifold commits, 30 to CRD-doc/manifest reads)

| Group | Kind | SelectorPath | ResolvedPath | Reason | CitationMR |
|---|---|---|---|---|---|
| autoscaling.aws.upbound.io | AutoscalingGroup | spec.forProvider.vpcZoneIdentifierSelector | spec.forProvider.vpcZoneIdentifier | Crossplane resolves selector to subnet-ID list; Argo sees it as "added" and fights forever | commit `3ea4b28d0` (`!1344`) |
| autoscaling.aws.upbound.io | AutoscalingGroup | spec.forProvider.vpcZoneIdentifierSelector | spec.forProvider.vpcZoneIdentifierRefs | Sibling refs array also populated by Crossplane; same drift as the primitive | commit `037988f54` (`!1250`) |
| autoscaling.aws.upbound.io | AutoscalingGroup | spec.forProvider.launchTemplate[].idSelector | spec.forProvider.launchTemplate[].id | Selector resolves to LaunchTemplate ID; Argo fights the resolved literal | commit `7aa6deb4e` (`!1341`) |
| autoscaling.aws.upbound.io | AutoscalingGroup | spec.forProvider.launchTemplate[].idSelector | spec.forProvider.launchTemplate[].idRef | Sibling Ref field populated alongside the ID; drift observed | commit `7ac5a5f14` (`!883`) |
| autoscaling.aws.upbound.io | AutoscalingGroup | spec.forProvider.launchTemplate[].idSelector | spec.forProvider.launchTemplate[].name | Upbound sometimes late-inits the name field when ID is selector-resolved | commit `7887c0510` |
| autoscaling.aws.upbound.io | Attachment | spec.forProvider.autoscalingGroupNameSelector | spec.forProvider.autoscalingGroupName | Selector resolves to ASG name; Argo drift on attachment | CRD-doc (`deploy/facilitygrid/ops/applications/crossplane-platform-aws-prod/.../egress-proxy-prod.yaml`) |
| autoscaling.aws.upbound.io | Attachment | spec.forProvider.lbTargetGroupArnSelector | spec.forProvider.lbTargetGroupArn | Selector resolves to target-group ARN on ASG→LB attachment | CRD-doc (same egress-proxy manifest) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.networkInterfaces[].subnetIdSelector | spec.forProvider.networkInterfaces[].subnetId | Late-init resolves subnet selector into concrete ID on network interface | commit `36ca77765` (`!890`) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.networkInterfaces[].subnetIdSelector | spec.forProvider.networkInterfaces[].subnetIdRef | Ref sibling populated by same resolution | commit `36ca77765` (`!890`) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.networkInterfaces[].securityGroupSelector | spec.forProvider.networkInterfaces[].securityGroups | Resolves selector to securityGroups (not `groups` — real field is `securityGroups`, see commit `67e97ed7d`) | commit `36ca77765` / `bf4d9244e` (`!890`) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.networkInterfaces[].securityGroupSelector | spec.forProvider.networkInterfaces[].securityGroupRefs | Sibling refs array populated | commit `36ca77765` (`!890`) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.iamInstanceProfile[].nameSelector | spec.forProvider.iamInstanceProfile[].name | Late-init resolves IAM instance profile reference | commit `36ca77765` (`!890`) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.iamInstanceProfile[].nameSelector | spec.forProvider.iamInstanceProfile[].nameRef | Ref sibling | commit `36ca77765` (`!890`) |
| ec2.aws.upbound.io | LaunchTemplate | spec.forProvider.iamInstanceProfile[].arnSelector | spec.forProvider.iamInstanceProfile[].arn | Upbound also resolves the sibling ARN path | commit `36ca77765` (`!890`) |
| ec2.aws.upbound.io | VPCEndpoint | spec.forProvider.vpcIdSelector | spec.forProvider.vpcId | Selector resolves to VPC ID; removed from live spec after reconcile | commit `7ac5a5f14` (`!1250`) |
| ec2.aws.upbound.io | VPCEndpoint | spec.forProvider.subnetIdSelector | spec.forProvider.subnetIds | Selector → plural subnet IDs on Interface endpoints; Argo sees list as added | commit `7ac5a5f14` (`!1250`) |
| ec2.aws.upbound.io | VPCEndpoint | spec.forProvider.routeTableIdSelector | spec.forProvider.routeTableIds | Selector → plural route-table IDs on Gateway endpoints | commit `7ac5a5f14` (`!1250`) |
| ec2.aws.upbound.io | VPCEndpoint | spec.forProvider.securityGroupIdSelector | spec.forProvider.securityGroupIds | Selector → plural SG IDs on Interface endpoints | commit `7ac5a5f14` (`!1250`) |
| ec2.aws.upbound.io | SecurityGroup | spec.forProvider.vpcIdSelector | spec.forProvider.vpcId | Selector resolves to concrete VPC ID | CRD-doc (`securitygroups-prod.yaml`) |
| ec2.aws.upbound.io | SecurityGroupRule | spec.forProvider.securityGroupIdSelector | spec.forProvider.securityGroupId | Selector resolves to the referenced SG's ID | CRD-doc (`securitygroups-prod.yaml`) |
| ec2.aws.upbound.io | SecurityGroupRule | spec.forProvider.sourceSecurityGroupIdSelector | spec.forProvider.sourceSecurityGroupId | Cross-SG reference selector → concrete ID | CRD-doc (`securitygroups-prod.yaml`) |
| ec2.aws.upbound.io | Route | spec.forProvider.routeTableIdSelector | spec.forProvider.routeTableId | Route's parent table selector → ID | CRD-doc (`vpc-prod.yaml`) |
| ec2.aws.upbound.io | Route | spec.forProvider.gatewayIdSelector | spec.forProvider.gatewayId | IGW/VGW selector → concrete gateway ID | CRD-doc (`vpc-prod.yaml`) |
| ec2.aws.upbound.io | Route | spec.forProvider.natGatewayIdSelector | spec.forProvider.natGatewayId | NAT-gateway selector → concrete ID | CRD-doc (`vpc-prod.yaml`) |
| ec2.aws.upbound.io | Route | spec.forProvider.vpcPeeringConnectionIdSelector | spec.forProvider.vpcPeeringConnectionId | Peering-connection selector → concrete ID | CRD-doc (`vpc-peering-ops-preview.yaml`) |
| ec2.aws.upbound.io | RouteTableAssociation | spec.forProvider.routeTableIdSelector | spec.forProvider.routeTableId | RT association selector → ID | CRD-doc (`vpc-prod.yaml`) |
| ec2.aws.upbound.io | RouteTableAssociation | spec.forProvider.subnetIdSelector | spec.forProvider.subnetId | Subnet assoc selector → ID | CRD-doc (`vpc-prod.yaml`) |
| ec2.aws.upbound.io | NATGateway | spec.forProvider.allocationIdSelector | spec.forProvider.allocationId | EIP allocation selector → concrete allocation ID | CRD-doc (`vpc-prod.yaml`) |
| ec2.aws.upbound.io | NATGateway | spec.forProvider.subnetIdSelector | spec.forProvider.subnetId | NAT placement subnet selector → ID | CRD-doc (`vpc-prod.yaml`) |
| elbv2.aws.upbound.io | LB | spec.forProvider.securityGroupSelector | spec.forProvider.securityGroups | Selector → plural SG IDs; Argo sees list as added | commit `3ea4b28d0` ALB block (`!1344`) |
| elbv2.aws.upbound.io | LB | spec.forProvider.subnetMapping[].subnetIdSelector | spec.forProvider.subnetMapping[].subnetId | Subnet-mapping selector → ID (ALB subnet list) | commit `3ea4b28d0` ALB block (`!1344`) |
| elbv2.aws.upbound.io | LB | spec.forProvider.subnetMapping[].subnetIdSelector | spec.forProvider.subnetMapping[].subnetIdRef | Sibling Ref field populated | commit `3ea4b28d0` ALB block (`!1344`) |
| elbv2.aws.upbound.io | LB | spec.forProvider.subnetSelector | spec.forProvider.subnets | Shorthand subnet selector on LB → plural subnets | commit `3ea4b28d0` ALB block (`!1344`) |
| elbv2.aws.upbound.io | LBListener | spec.forProvider.loadBalancerArnSelector | spec.forProvider.loadBalancerArn | Listener's parent LB selector → concrete ARN | CRD-doc (`alb-prod.yaml`) |
| elbv2.aws.upbound.io | LBListenerCertificate | spec.forProvider.listenerArnSelector | spec.forProvider.listenerArn | Listener ref selector → ARN | CRD-doc (`alb-prod.yaml`) |
| elbv2.aws.upbound.io | LBListenerRule | spec.forProvider.action[].targetGroupArnSelector | spec.forProvider.action[].targetGroupArn | Forward-action target-group selector → ARN (Helm-templated in fargateapp composition) | CRD-doc (`composition-fargateapp-prod.yaml`) |
| rds.aws.upbound.io | ClusterInstance | spec.forProvider.clusterIdentifierSelector | spec.forProvider.clusterIdentifier | ClusterInstance's parent Cluster selector → identifier | CRD-doc (`cluster-instance.yaml`) |
| rds.aws.upbound.io | ClusterInstance | spec.forProvider.monitoringRoleArnSelector | spec.forProvider.monitoringRoleArn | IAM role ARN selector → concrete ARN | CRD-doc (`cluster-instance.yaml`) |
| rds.aws.upbound.io | Cluster | spec.forProvider.vpcSecurityGroupIdSelector | spec.forProvider.vpcSecurityGroupIds | Selector → plural SG IDs on Aurora cluster | CRD-doc (`aurora-prod-cluster.yaml`) |
| rds.aws.upbound.io | ProxyDefaultTargetGroup | spec.forProvider.dbProxyNameSelector | spec.forProvider.dbProxyName | RDS proxy name selector → concrete name; parent MR fixed this for prod | commit `7ac5a5f14` (`!1250`) |
| rds.aws.upbound.io | ProxyTarget | spec.forProvider.dbProxyNameSelector | spec.forProvider.dbProxyName | Same as ProxyDefaultTargetGroup | commit `7ac5a5f14` (`!1250`) |
| rds.aws.upbound.io | Proxy | spec.forProvider.roleArnSelector | spec.forProvider.roleArn | IAM role ARN selector → concrete ARN | CRD-doc (`rds-proxy-prod.yaml`) |
| rds.aws.upbound.io | Proxy | spec.forProvider.vpcSecurityGroupIdSelector | spec.forProvider.vpcSecurityGroupIds | VPC SG selector → plural IDs | CRD-doc (`rds-proxy-prod.yaml`) |
| elasticache.aws.upbound.io | ReplicationGroup | spec.forProvider.subnetGroupNameSelector | spec.forProvider.subnetGroupName | Subnet-group selector → concrete name | CRD-doc (`elasticache-prod-cluster.yaml`) |
| elasticache.aws.upbound.io | ReplicationGroup | spec.forProvider.parameterGroupNameSelector | spec.forProvider.parameterGroupName | ParameterGroup selector drift; fg-manifold gave up and inlined the literal name in `5e855ea4b` | commit `5e855ea4b` (`!1366`) |
| dms.aws.upbound.io | ReplicationInstance | spec.forProvider.replicationSubnetGroupIdSelector | spec.forProvider.replicationSubnetGroupId | DMS subnet-group selector → concrete ID | CRD-doc (`dms-prod-replication-instance.yaml`) |
| dms.aws.upbound.io | ReplicationInstance | spec.forProvider.vpcSecurityGroupIdSelector | spec.forProvider.vpcSecurityGroupIds | VPC SG selector → plural IDs | CRD-doc (`dms-prod-replication-instance.yaml`) |
| iam.aws.upbound.io | RolePolicyAttachment | spec.forProvider.policyArnSelector | spec.forProvider.policyArn | Policy ARN selector → concrete ARN | CRD-doc (`irsa-role.yaml`) |
| iam.aws.upbound.io | RolePolicyAttachment | spec.forProvider.roleSelector | spec.forProvider.role | Role name selector → concrete name | CRD-doc (`irsa-role.yaml`) |
| kms.aws.upbound.io | Alias | spec.forProvider.targetKeyIdSelector | spec.forProvider.targetKeyId | KMS key-ID selector → concrete ID | CRD-doc (`kms-key.yaml`) |
| secretsmanager.aws.upbound.io | SecretVersion | spec.forProvider.secretIdSelector | spec.forProvider.secretId | Secret selector → concrete secret ID/ARN | CRD-doc (`aurora-prod-app-user.yaml`) |
| wafv2.aws.upbound.io | WebACLAssociation | spec.forProvider.webAclArnSelector | spec.forProvider.webAclArn | WAF WebACL selector → concrete ARN | CRD-doc (`alb-prod.yaml`) |
| projects.gitlab.m.crossplane.io | Project | spec.forProvider.namespaceIdSelector | spec.forProvider.namespaceId | GitLab namespace selector → concrete numeric ID | CRD-doc (`projects/fg-ai.yaml`) fg-manifold uses `namespaceIdRef` directly; mapping derived from `!36` MR subject + field naming convention |
