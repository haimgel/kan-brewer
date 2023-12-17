package config

const BlueprintAnnotationName = "kan-brewer.haim.dev/kanister-blueprints"
const ManagedByLabel = "app.kubernetes.io/managed-by"
const AppId = "kan-brewer"

type Config struct {
	ActionSetNamespace      string
	KeepCompletedActionSets int64
}
