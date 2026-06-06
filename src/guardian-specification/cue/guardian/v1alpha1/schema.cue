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
		nomad: run: #LifecycleHook
	}
}

#Substrate: {
	apiVersion: "substrate.guardianintelligence.org/v1alpha1"
	kind:       "Substrate"
	metadata:   #Metadata
	spec: {
		access: #LifecycleHook
		upload: {
			run:     #LifecycleHook
			extract: #LifecycleHook
			verify:  #LifecycleHook
		}
		kernel: {
			openbaoPrepare: #LifecycleHook
			nomad:          #LifecycleHook
			verify:         #LifecycleHook
		}
		remote?: {
			repoRoot: #AbsolutePath
			guardian: #AbsolutePath
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

#LifecycleHook: {
	argv: [#NonEmptyString, ...#NonEmptyString]
}

#AbsolutePath: string & =~"^/.+"
