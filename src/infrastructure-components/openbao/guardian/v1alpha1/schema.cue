package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#OpenBaoCluster: {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "OpenBaoCluster"
	metadata:   #Metadata
	spec: {
		address:          #HTTPURL
		caCert:           #AbsolutePath
		runtimeRoot:      #AbsolutePath
		dataDir:          #AbsolutePath
		configPath:       #AbsolutePath
		reportPath:       #AbsolutePath
		initMaterialPath: #AbsolutePath
		loopInterval:     #NonEmptyString
		seal: {
			shamir: {
				keyShares:    int & >0
				keyThreshold: int & >0 & <=keyShares
				pgpRecipientRefs: [#ObjectRef & {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "PGPRecipient"
				}, ...]
				rootTokenRecipientRef: #ObjectRef & {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "PGPRecipient"
				}
			}
		}
		snapshots?: {
			restore?: {
				manifestPath?: #AbsolutePath
				snapshotPath?: #AbsolutePath
				sourceRef?:    #ObjectRef
			}
			save?: {
				manifestPath:    #AbsolutePath
				snapshotPath:    #AbsolutePath
				destinationRef?: #ObjectRef
			}
		}
		baseline?: {
			reconcile: bool
			mounts?: [...{
				path:         #NonEmptyString
				type:         #NonEmptyString
				description?: #NonEmptyString
				options?: [#NonEmptyString]: #NonEmptyString
			}]
			policies?: [...{
				name: #NonEmptyString
				hcl:  #NonEmptyString
			}]
			nomadJWT?: {
				path:         #NonEmptyString
				description?: #NonEmptyString
				jwksURL:      #HTTPURL
				supportedAlgs: [#NonEmptyString, ...#NonEmptyString]
				roles: [...{
					name:     #NonEmptyString
					roleType: "jwt"
					boundAudiences: [#NonEmptyString, ...#NonEmptyString]
					boundClaims?: [#NonEmptyString]: #NonEmptyString
					userClaim:            #NonEmptyString
					userClaimJSONPointer: bool
					claimMappings?: [#NonEmptyString]: #NonEmptyString
					tokenType: #NonEmptyString
					tokenPolicies: [#NonEmptyString, ...#NonEmptyString]
					tokenPeriod:         #NonEmptyString
					tokenExplicitMaxTTL: int & >=0
				}]
			}
		}
	}
}

#PGPRecipient: {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "PGPRecipient"
	metadata:   #Metadata
	spec: {
		publicKeyBase64: #NonEmptyString
	}
}
