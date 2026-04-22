package ir

import "github.com/pyrex41/cross-validate-/pkg/types"

// SelectorRegistry returns the static catalog of Crossplane selector fields
// and the concrete sibling paths they resolve to via late-init. Expand by
// appending to this slice — one entry per (Group, Kind, SelectorPath, ResolvedPath).
//
// Array-indexed paths use "[]" as a wildcard placeholder segment, e.g.
// "spec.forProvider.launchTemplate[].idSelector". These expand through
// ir.WalkPath during extractSelectorUsages — each declared array element
// produces its own SelectorUsage with the ResolvedPath specialized to the
// matching concrete index.
//
// Citations are anchored to fg-manifold commit SHAs or "CRD-doc" reads;
// the commit SHAs map to GitLab MRs noted in parentheses.
func SelectorRegistry() []types.SelectorMapping {
	return []types.SelectorMapping{
		// ── autoscaling.aws.upbound.io / AutoscalingGroup ──────────────────────
		// commit 3ea4b28d0 (!1344): vpcZoneIdentifier selector drift suppressed.
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "AutoscalingGroup",
			SelectorPath: "spec.forProvider.vpcZoneIdentifierSelector",
			ResolvedPath: "spec.forProvider.vpcZoneIdentifier",
			Reason:       "Crossplane resolves selector to subnet-ID list; Argo sees it as 'added' and fights forever. commit 3ea4b28d0 (!1344)",
		},
		// commit 037988f54 (!1250): sibling vpcZoneIdentifierRefs also suppressed.
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "AutoscalingGroup",
			SelectorPath: "spec.forProvider.vpcZoneIdentifierSelector",
			ResolvedPath: "spec.forProvider.vpcZoneIdentifierRefs",
			Reason:       "Sibling Refs array also populated by Crossplane; same drift as the primitive. commit 037988f54 (!1250)",
		},
		// commit 7aa6deb4e (!1341): launchTemplate[].idSelector — array-indexed path.
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "AutoscalingGroup",
			SelectorPath: "spec.forProvider.launchTemplate[].idSelector",
			ResolvedPath: "spec.forProvider.launchTemplate[].id",
			Reason:       "Selector resolves to LaunchTemplate ID; Argo fights the resolved literal. commit 7aa6deb4e (!1341)",
		},
		// commit 7ac5a5f14 (!883): sibling idRef array-indexed path.
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "AutoscalingGroup",
			SelectorPath: "spec.forProvider.launchTemplate[].idSelector",
			ResolvedPath: "spec.forProvider.launchTemplate[].idRef",
			Reason:       "Sibling Ref field populated alongside the ID; drift observed. commit 7ac5a5f14 (!883)",
		},
		// commit 7887c0510: Upbound late-inits name field when ID is selector-resolved.
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "AutoscalingGroup",
			SelectorPath: "spec.forProvider.launchTemplate[].idSelector",
			ResolvedPath: "spec.forProvider.launchTemplate[].name",
			Reason:       "Upbound sometimes late-inits the name field when ID is selector-resolved. commit 7887c0510",
		},

		// ── autoscaling.aws.upbound.io / Attachment ────────────────────────────
		// CRD-doc (egress-proxy-prod.yaml in fg-manifold ops manifests).
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "Attachment",
			SelectorPath: "spec.forProvider.autoscalingGroupNameSelector",
			ResolvedPath: "spec.forProvider.autoscalingGroupName",
			Reason:       "Selector resolves to ASG name; Argo drift on attachment. CRD-doc (egress-proxy-prod.yaml)",
		},
		{
			Group:        "autoscaling.aws.upbound.io",
			Kind:         "Attachment",
			SelectorPath: "spec.forProvider.lbTargetGroupArnSelector",
			ResolvedPath: "spec.forProvider.lbTargetGroupArn",
			Reason:       "Selector resolves to target-group ARN on ASG→LB attachment. CRD-doc (egress-proxy-prod.yaml)",
		},

		// ── ec2.aws.upbound.io / LaunchTemplate ───────────────────────────────
		// commit 36ca77765 (!890): network interface selector drift.
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.networkInterfaces[].subnetIdSelector",
			ResolvedPath: "spec.forProvider.networkInterfaces[].subnetId",
			Reason:       "Late-init resolves subnet selector into concrete ID on network interface. commit 36ca77765 (!890)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.networkInterfaces[].subnetIdSelector",
			ResolvedPath: "spec.forProvider.networkInterfaces[].subnetIdRef",
			Reason:       "Ref sibling populated by same resolution. commit 36ca77765 (!890)",
		},
		// commit 36ca77765 / bf4d9244e (!890): real field is securityGroups not groups.
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.networkInterfaces[].securityGroupSelector",
			ResolvedPath: "spec.forProvider.networkInterfaces[].securityGroups",
			Reason:       "Resolves selector to securityGroups (real field, not 'groups'). commit 36ca77765 / bf4d9244e (!890)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.networkInterfaces[].securityGroupSelector",
			ResolvedPath: "spec.forProvider.networkInterfaces[].securityGroupRefs",
			Reason:       "Sibling Refs array populated. commit 36ca77765 (!890)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.iamInstanceProfile[].nameSelector",
			ResolvedPath: "spec.forProvider.iamInstanceProfile[].name",
			Reason:       "Late-init resolves IAM instance profile reference. commit 36ca77765 (!890)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.iamInstanceProfile[].nameSelector",
			ResolvedPath: "spec.forProvider.iamInstanceProfile[].nameRef",
			Reason:       "Ref sibling. commit 36ca77765 (!890)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "LaunchTemplate",
			SelectorPath: "spec.forProvider.iamInstanceProfile[].arnSelector",
			ResolvedPath: "spec.forProvider.iamInstanceProfile[].arn",
			Reason:       "Upbound also resolves the sibling ARN path. commit 36ca77765 (!890)",
		},

		// ── ec2.aws.upbound.io / VPCEndpoint ─────────────────────────────────
		// commit 7ac5a5f14 (!1250): VPC endpoint selector drift.
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "VPCEndpoint",
			SelectorPath: "spec.forProvider.vpcIdSelector",
			ResolvedPath: "spec.forProvider.vpcId",
			Reason:       "Selector resolves to VPC ID; removed from live spec after reconcile. commit 7ac5a5f14 (!1250)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "VPCEndpoint",
			SelectorPath: "spec.forProvider.subnetIdSelector",
			ResolvedPath: "spec.forProvider.subnetIds",
			Reason:       "Selector → plural subnet IDs on Interface endpoints; Argo sees list as added. commit 7ac5a5f14 (!1250)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "VPCEndpoint",
			SelectorPath: "spec.forProvider.routeTableIdSelector",
			ResolvedPath: "spec.forProvider.routeTableIds",
			Reason:       "Selector → plural route-table IDs on Gateway endpoints. commit 7ac5a5f14 (!1250)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "VPCEndpoint",
			SelectorPath: "spec.forProvider.securityGroupIdSelector",
			ResolvedPath: "spec.forProvider.securityGroupIds",
			Reason:       "Selector → plural SG IDs on Interface endpoints. commit 7ac5a5f14 (!1250)",
		},

		// ── ec2.aws.upbound.io / SecurityGroup ───────────────────────────────
		// CRD-doc (securitygroups-prod.yaml).
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "SecurityGroup",
			SelectorPath: "spec.forProvider.vpcIdSelector",
			ResolvedPath: "spec.forProvider.vpcId",
			Reason:       "Selector resolves to concrete VPC ID. CRD-doc (securitygroups-prod.yaml)",
		},

		// ── ec2.aws.upbound.io / SecurityGroupRule ───────────────────────────
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "SecurityGroupRule",
			SelectorPath: "spec.forProvider.securityGroupIdSelector",
			ResolvedPath: "spec.forProvider.securityGroupId",
			Reason:       "Selector resolves to the referenced SG's ID. CRD-doc (securitygroups-prod.yaml)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "SecurityGroupRule",
			SelectorPath: "spec.forProvider.sourceSecurityGroupIdSelector",
			ResolvedPath: "spec.forProvider.sourceSecurityGroupId",
			Reason:       "Cross-SG reference selector → concrete ID. CRD-doc (securitygroups-prod.yaml)",
		},

		// ── ec2.aws.upbound.io / Route ───────────────────────────────────────
		// CRD-doc (vpc-prod.yaml).
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "Route",
			SelectorPath: "spec.forProvider.routeTableIdSelector",
			ResolvedPath: "spec.forProvider.routeTableId",
			Reason:       "Route's parent table selector → ID. CRD-doc (vpc-prod.yaml)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "Route",
			SelectorPath: "spec.forProvider.gatewayIdSelector",
			ResolvedPath: "spec.forProvider.gatewayId",
			Reason:       "IGW/VGW selector → concrete gateway ID. CRD-doc (vpc-prod.yaml)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "Route",
			SelectorPath: "spec.forProvider.natGatewayIdSelector",
			ResolvedPath: "spec.forProvider.natGatewayId",
			Reason:       "NAT-gateway selector → concrete ID. CRD-doc (vpc-prod.yaml)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "Route",
			SelectorPath: "spec.forProvider.vpcPeeringConnectionIdSelector",
			ResolvedPath: "spec.forProvider.vpcPeeringConnectionId",
			Reason:       "Peering-connection selector → concrete ID. CRD-doc (vpc-peering-ops-preview.yaml)",
		},

		// ── ec2.aws.upbound.io / RouteTableAssociation ───────────────────────
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "RouteTableAssociation",
			SelectorPath: "spec.forProvider.routeTableIdSelector",
			ResolvedPath: "spec.forProvider.routeTableId",
			Reason:       "RT association selector → ID. CRD-doc (vpc-prod.yaml)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "RouteTableAssociation",
			SelectorPath: "spec.forProvider.subnetIdSelector",
			ResolvedPath: "spec.forProvider.subnetId",
			Reason:       "Subnet assoc selector → ID. CRD-doc (vpc-prod.yaml)",
		},

		// ── ec2.aws.upbound.io / NATGateway ──────────────────────────────────
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "NATGateway",
			SelectorPath: "spec.forProvider.allocationIdSelector",
			ResolvedPath: "spec.forProvider.allocationId",
			Reason:       "EIP allocation selector → concrete allocation ID. CRD-doc (vpc-prod.yaml)",
		},
		{
			Group:        "ec2.aws.upbound.io",
			Kind:         "NATGateway",
			SelectorPath: "spec.forProvider.subnetIdSelector",
			ResolvedPath: "spec.forProvider.subnetId",
			Reason:       "NAT placement subnet selector → ID. CRD-doc (vpc-prod.yaml)",
		},

		// ── elbv2.aws.upbound.io / LB ─────────────────────────────────────────
		// commit 3ea4b28d0 (!1344): ALB selector drift.
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LB",
			SelectorPath: "spec.forProvider.securityGroupSelector",
			ResolvedPath: "spec.forProvider.securityGroups",
			Reason:       "Selector → plural SG IDs; Argo sees list as added. commit 3ea4b28d0 ALB block (!1344)",
		},
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LB",
			SelectorPath: "spec.forProvider.subnetMapping[].subnetIdSelector",
			ResolvedPath: "spec.forProvider.subnetMapping[].subnetId",
			Reason:       "Subnet-mapping selector → ID (ALB subnet list). commit 3ea4b28d0 ALB block (!1344)",
		},
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LB",
			SelectorPath: "spec.forProvider.subnetMapping[].subnetIdSelector",
			ResolvedPath: "spec.forProvider.subnetMapping[].subnetIdRef",
			Reason:       "Sibling Ref field populated. commit 3ea4b28d0 ALB block (!1344)",
		},
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LB",
			SelectorPath: "spec.forProvider.subnetSelector",
			ResolvedPath: "spec.forProvider.subnets",
			Reason:       "Shorthand subnet selector on LB → plural subnets. commit 3ea4b28d0 ALB block (!1344)",
		},

		// ── elbv2.aws.upbound.io / LBListener ────────────────────────────────
		// CRD-doc (alb-prod.yaml).
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LBListener",
			SelectorPath: "spec.forProvider.loadBalancerArnSelector",
			ResolvedPath: "spec.forProvider.loadBalancerArn",
			Reason:       "Listener's parent LB selector → concrete ARN. CRD-doc (alb-prod.yaml)",
		},

		// ── elbv2.aws.upbound.io / LBListenerCertificate ─────────────────────
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LBListenerCertificate",
			SelectorPath: "spec.forProvider.listenerArnSelector",
			ResolvedPath: "spec.forProvider.listenerArn",
			Reason:       "Listener ref selector → ARN. CRD-doc (alb-prod.yaml)",
		},

		// ── elbv2.aws.upbound.io / LBListenerRule ────────────────────────────
		// CRD-doc (composition-fargateapp-prod.yaml).
		{
			Group:        "elbv2.aws.upbound.io",
			Kind:         "LBListenerRule",
			SelectorPath: "spec.forProvider.action[].targetGroupArnSelector",
			ResolvedPath: "spec.forProvider.action[].targetGroupArn",
			Reason:       "Forward-action target-group selector → ARN. CRD-doc (composition-fargateapp-prod.yaml)",
		},

		// ── rds.aws.upbound.io / ClusterInstance ─────────────────────────────
		// CRD-doc (cluster-instance.yaml).
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "ClusterInstance",
			SelectorPath: "spec.forProvider.clusterIdentifierSelector",
			ResolvedPath: "spec.forProvider.clusterIdentifier",
			Reason:       "ClusterInstance's parent Cluster selector → identifier. CRD-doc (cluster-instance.yaml)",
		},
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "ClusterInstance",
			SelectorPath: "spec.forProvider.monitoringRoleArnSelector",
			ResolvedPath: "spec.forProvider.monitoringRoleArn",
			Reason:       "IAM role ARN selector → concrete ARN. CRD-doc (cluster-instance.yaml)",
		},

		// ── rds.aws.upbound.io / Cluster ─────────────────────────────────────
		// CRD-doc (aurora-prod-cluster.yaml).
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "Cluster",
			SelectorPath: "spec.forProvider.vpcSecurityGroupIdSelector",
			ResolvedPath: "spec.forProvider.vpcSecurityGroupIds",
			Reason:       "Selector → plural SG IDs on Aurora cluster. CRD-doc (aurora-prod-cluster.yaml)",
		},

		// ── rds.aws.upbound.io / ProxyDefaultTargetGroup ─────────────────────
		// commit 7ac5a5f14 (!1250): RDS proxy name drift.
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "ProxyDefaultTargetGroup",
			SelectorPath: "spec.forProvider.dbProxyNameSelector",
			ResolvedPath: "spec.forProvider.dbProxyName",
			Reason:       "RDS proxy name selector → concrete name. commit 7ac5a5f14 (!1250)",
		},

		// ── rds.aws.upbound.io / ProxyTarget ─────────────────────────────────
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "ProxyTarget",
			SelectorPath: "spec.forProvider.dbProxyNameSelector",
			ResolvedPath: "spec.forProvider.dbProxyName",
			Reason:       "Same as ProxyDefaultTargetGroup. commit 7ac5a5f14 (!1250)",
		},

		// ── rds.aws.upbound.io / Proxy ───────────────────────────────────────
		// CRD-doc (rds-proxy-prod.yaml).
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "Proxy",
			SelectorPath: "spec.forProvider.roleArnSelector",
			ResolvedPath: "spec.forProvider.roleArn",
			Reason:       "IAM role ARN selector → concrete ARN. CRD-doc (rds-proxy-prod.yaml)",
		},
		{
			Group:        "rds.aws.upbound.io",
			Kind:         "Proxy",
			SelectorPath: "spec.forProvider.vpcSecurityGroupIdSelector",
			ResolvedPath: "spec.forProvider.vpcSecurityGroupIds",
			Reason:       "VPC SG selector → plural IDs. CRD-doc (rds-proxy-prod.yaml)",
		},

		// ── elasticache.aws.upbound.io / ReplicationGroup ─────────────────────
		// CRD-doc (elasticache-prod-cluster.yaml).
		{
			Group:        "elasticache.aws.upbound.io",
			Kind:         "ReplicationGroup",
			SelectorPath: "spec.forProvider.subnetGroupNameSelector",
			ResolvedPath: "spec.forProvider.subnetGroupName",
			Reason:       "Subnet-group selector → concrete name. CRD-doc (elasticache-prod-cluster.yaml)",
		},
		// commit 5e855ea4b (!1366): parameter group drift.
		{
			Group:        "elasticache.aws.upbound.io",
			Kind:         "ReplicationGroup",
			SelectorPath: "spec.forProvider.parameterGroupNameSelector",
			ResolvedPath: "spec.forProvider.parameterGroupName",
			Reason:       "ParameterGroup selector drift; fg-manifold inlined literal name in 5e855ea4b (!1366)",
		},

		// ── dms.aws.upbound.io / ReplicationInstance ──────────────────────────
		// CRD-doc (dms-prod-replication-instance.yaml).
		{
			Group:        "dms.aws.upbound.io",
			Kind:         "ReplicationInstance",
			SelectorPath: "spec.forProvider.replicationSubnetGroupIdSelector",
			ResolvedPath: "spec.forProvider.replicationSubnetGroupId",
			Reason:       "DMS subnet-group selector → concrete ID. CRD-doc (dms-prod-replication-instance.yaml)",
		},
		{
			Group:        "dms.aws.upbound.io",
			Kind:         "ReplicationInstance",
			SelectorPath: "spec.forProvider.vpcSecurityGroupIdSelector",
			ResolvedPath: "spec.forProvider.vpcSecurityGroupIds",
			Reason:       "VPC SG selector → plural IDs. CRD-doc (dms-prod-replication-instance.yaml)",
		},

		// ── iam.aws.upbound.io / RolePolicyAttachment ────────────────────────
		// CRD-doc (irsa-role.yaml).
		{
			Group:        "iam.aws.upbound.io",
			Kind:         "RolePolicyAttachment",
			SelectorPath: "spec.forProvider.policyArnSelector",
			ResolvedPath: "spec.forProvider.policyArn",
			Reason:       "Policy ARN selector → concrete ARN. CRD-doc (irsa-role.yaml)",
		},
		{
			Group:        "iam.aws.upbound.io",
			Kind:         "RolePolicyAttachment",
			SelectorPath: "spec.forProvider.roleSelector",
			ResolvedPath: "spec.forProvider.role",
			Reason:       "Role name selector → concrete name. CRD-doc (irsa-role.yaml)",
		},

		// ── kms.aws.upbound.io / Alias ───────────────────────────────────────
		// CRD-doc (kms-key.yaml).
		{
			Group:        "kms.aws.upbound.io",
			Kind:         "Alias",
			SelectorPath: "spec.forProvider.targetKeyIdSelector",
			ResolvedPath: "spec.forProvider.targetKeyId",
			Reason:       "KMS key-ID selector → concrete ID. CRD-doc (kms-key.yaml)",
		},

		// ── secretsmanager.aws.upbound.io / SecretVersion ────────────────────
		// CRD-doc (aurora-prod-app-user.yaml).
		{
			Group:        "secretsmanager.aws.upbound.io",
			Kind:         "SecretVersion",
			SelectorPath: "spec.forProvider.secretIdSelector",
			ResolvedPath: "spec.forProvider.secretId",
			Reason:       "Secret selector → concrete secret ID/ARN. CRD-doc (aurora-prod-app-user.yaml)",
		},

		// ── wafv2.aws.upbound.io / WebACLAssociation ─────────────────────────
		// CRD-doc (alb-prod.yaml).
		{
			Group:        "wafv2.aws.upbound.io",
			Kind:         "WebACLAssociation",
			SelectorPath: "spec.forProvider.webAclArnSelector",
			ResolvedPath: "spec.forProvider.webAclArn",
			Reason:       "WAF WebACL selector → concrete ARN. CRD-doc (alb-prod.yaml)",
		},

		// ── projects.gitlab.m.crossplane.io / Project ─────────────────────────
		// CRD-doc (fg-ai.yaml) + naming convention from !36 MR subject.
		{
			Group:        "projects.gitlab.m.crossplane.io",
			Kind:         "Project",
			SelectorPath: "spec.forProvider.namespaceIdSelector",
			ResolvedPath: "spec.forProvider.namespaceId",
			Reason:       "GitLab namespace selector → concrete numeric ID. CRD-doc (projects/fg-ai.yaml); convention from !36",
		},
	}
}
