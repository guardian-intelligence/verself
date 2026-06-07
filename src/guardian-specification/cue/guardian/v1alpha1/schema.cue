package v1alpha1

#NonEmptyString: string & !=""
#HTTPSOrigin:    =~"^https://[^\\s/$.?#][^\\s/?#]*$"

#Document: {
	entrypoint: #ObjectRef & {
		apiVersion: "guardian.guardianintelligence.org/v1alpha1"
		kind:       "FlyProcedure"
	}
	resources: [#Resource, ...#Resource]
}

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#Resource: #FlyProcedure | #Substrate | #PublicOrigin | #ExtensionResource

#ExtensionResource: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	metadata:   #Metadata
	spec?: {...}

	if apiVersion == "guardian.guardianintelligence.org/v1alpha1" {
		kind: !="FlyProcedure"
	}
	if apiVersion == "substrate.guardianintelligence.org/v1alpha1" {
		kind: !="Substrate"
	}
	if apiVersion == "networking.guardianintelligence.org/v1alpha1" {
		kind: !="PublicOrigin"
	}
}

#FlyProcedure: {
	apiVersion: "guardian.guardianintelligence.org/v1alpha1"
	kind:       "FlyProcedure"
	metadata:   #Metadata
	spec: {
		substrateRef: #ObjectRef & {
			apiVersion: "substrate.guardianintelligence.org/v1alpha1"
			kind:       "Substrate"
		}
		preflight: #Preflight
		nomad: #NomadFly
	}
}

#Substrate: {
	apiVersion: "substrate.guardianintelligence.org/v1alpha1"
	kind:       "Substrate"
	metadata:   #Metadata
	spec: {
		remote?: {
			repoRoot: #AbsolutePath
			ssh: [#NonEmptyString, ...#NonEmptyString]
		}
	}
}

#PublicOrigin: {
	apiVersion: "networking.guardianintelligence.org/v1alpha1"
	kind:       "PublicOrigin"
	metadata:   #Metadata
	spec: {
		url: #HTTPSOrigin
	}
}

#Preflight: {
	ansible: {
		playbook: #WorkspaceRelativePath
	}
}

#NomadFly: {
	address:   #HTTPURL
	namespace: #NonEmptyString
	jobs: [#NomadJob, ...#NomadJob]
}

#NomadJob: {
	name:          #NonEmptyString
	path:          #WorkspaceRelativePath
	artifactPaths?: [...#WorkspaceRelativePath]
}

#WorkspaceRelativePath: string & !="" & !~"^/" & !~"(^|/)\\.\\.(/|$)"
#AbsolutePath:          string & =~"^/.+"
#HTTPURL:               =~"^https?://[^\\s]+$"
