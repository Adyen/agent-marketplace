{{- define "agent-marketplace.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "agent-marketplace.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := include "agent-marketplace.name" . }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "agent-marketplace.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "agent-marketplace.labels" -}}
helm.sh/chart: {{ include "agent-marketplace.chart" . }}
{{ include "agent-marketplace.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "agent-marketplace.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agent-marketplace.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "agent-marketplace.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "agent-marketplace.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "agent-marketplace.componentName" -}}
{{- printf "%s-%s" (include "agent-marketplace.fullname" .root) .component | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "agent-marketplace.configMapName" -}}
{{- default (printf "%s-config" (include "agent-marketplace.fullname" .)) .Values.config.existingConfigMap }}
{{- end }}

{{- define "agent-marketplace.apiComponent" -}}
{{- if eq .Values.mode "all" }}all{{ else }}api{{ end }}
{{- end }}

{{- define "agent-marketplace.servicePort" -}}
{{- if eq .component "api" }}{{ .root.Values.service.api.port }}
{{- else if eq .component "web" }}{{ .root.Values.service.web.port }}
{{- else }}{{ .root.Values.service.site.port }}{{ end }}
{{- end }}

{{- define "agent-marketplace.image" -}}
{{- $root := .root -}}
{{- $image := mergeOverwrite (deepCopy $root.Values.image) (default (dict) .image) -}}
{{- if $image.digest }}
{{- printf "%s@%s" $image.repository $image.digest }}
{{- else }}
{{- printf "%s:%s" $image.repository $image.tag }}
{{- end }}
{{- end }}

{{- define "agent-marketplace.validate" -}}
{{- if not (has .Values.mode (list "split" "all")) }}
{{- fail "mode must be either split or all" }}
{{- end }}
{{- if and (eq .Values.mode "split") (not (or .Values.api.enabled .Values.worker.enabled)) }}
{{- fail "at least one of api.enabled or worker.enabled must be true in split mode" }}
{{- end }}
{{- if and .Values.web.enabled (eq .Values.mode "split") (not .Values.api.enabled) }}
{{- fail "web.enabled requires api.enabled in split mode" }}
{{- end }}
{{- if and .Values.serviceMonitor.enabled (eq .Values.mode "split") (not .Values.api.enabled) }}
{{- fail "serviceMonitor.enabled requires api.enabled in split mode" }}
{{- end }}
{{- if and .Values.networkPolicy.enabled (not (or .Values.networkPolicy.allowSameNamespace (gt (len .Values.networkPolicy.ingressNamespaces) 0))) }}
{{- fail "networkPolicy requires allowSameNamespace or at least one ingressNamespaces entry" }}
{{- end }}
{{- end }}

{{- define "agent-marketplace.deployment" -}}
{{- $root := .root -}}
{{- $component := .component -}}
{{- $role := .role -}}
{{- $isMarketplace := has $role (list "api" "worker" "all") -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "agent-marketplace.componentName" (dict "root" $root "component" $role) }}
  labels:
    {{- include "agent-marketplace.labels" $root | nindent 4 }}
    app.kubernetes.io/component: {{ $role }}
spec:
  replicas: {{ $component.replicas }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      {{- include "agent-marketplace.selectorLabels" $root | nindent 6 }}
      app.kubernetes.io/component: {{ $role }}
  template:
    metadata:
      labels:
        {{- include "agent-marketplace.selectorLabels" $root | nindent 8 }}
        app.kubernetes.io/component: {{ $role }}
        {{- with $root.Values.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      {{- with $root.Values.podAnnotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
    spec:
      serviceAccountName: {{ include "agent-marketplace.serviceAccountName" $root }}
      automountServiceAccountToken: {{ $root.Values.serviceAccount.automountServiceAccountToken }}
      terminationGracePeriodSeconds: {{ $root.Values.terminationGracePeriodSeconds }}
      {{- with $root.Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      securityContext:
        {{- toYaml $root.Values.podSecurityContext | nindent 8 }}
      {{- with $root.Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $root.Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $root.Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $root.Values.topologySpreadConstraints }}
      topologySpreadConstraints:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ $role }}
          image: {{ include "agent-marketplace.image" (dict "root" $root "image" $component.image) }}
          imagePullPolicy: {{ default $root.Values.image.pullPolicy $component.image.pullPolicy }}
          {{- with $component.command }}
          command:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with $component.args }}
          args:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- if or $isMarketplace (eq $role "web") (eq $role "site") }}
          {{- if or (eq $role "api") (eq $role "all") }}
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
            - name: operations
              containerPort: 9090
              protocol: TCP
          {{- else if eq $role "web" }}
          ports:
            - name: http
              containerPort: 3000
              protocol: TCP
          {{- else if eq $role "site" }}
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          {{- end }}
          {{- end }}
          {{- if or (and $isMarketplace $root.Values.storage.dsnSecret.name) (eq $role "web") (gt (len $component.env) 0) }}
          env:
            {{- if and $isMarketplace $root.Values.storage.dsnSecret.name }}
            - name: {{ $root.Values.storage.dsnSecret.envName }}
              valueFrom:
                secretKeyRef:
                  name: {{ $root.Values.storage.dsnSecret.name }}
                  key: {{ $root.Values.storage.dsnSecret.key }}
            {{- end }}
            {{- if eq $role "web" }}
            - name: NUXT_APP_BASE_URL
              value: {{ $root.Values.web.basePath | quote }}
            - name: NUXT_PUBLIC_API_BASE
              value: {{ $root.Values.web.publicApiBase | quote }}
            - name: NUXT_API_BASE_INTERNAL
              value: {{ printf "http://%s:%v" (include "agent-marketplace.componentName" (dict "root" $root "component" "api")) $root.Values.service.api.port | quote }}
            {{- end }}
            {{- with $component.env }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
          {{- end }}
          {{- with $component.envFrom }}
          envFrom:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          securityContext:
            {{- toYaml $root.Values.containerSecurityContext | nindent 12 }}
          {{- with $component.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- if $component.probes.liveness.enabled }}
          livenessProbe:
            httpGet:
              path: {{ $component.probes.liveness.path }}
              port: {{ $component.probes.liveness.port }}
            initialDelaySeconds: {{ $component.probes.liveness.initialDelaySeconds }}
            periodSeconds: {{ $component.probes.liveness.periodSeconds }}
            timeoutSeconds: {{ $component.probes.liveness.timeoutSeconds }}
            failureThreshold: {{ $component.probes.liveness.failureThreshold }}
          {{- end }}
          {{- if $component.probes.readiness.enabled }}
          readinessProbe:
            httpGet:
              path: {{ $component.probes.readiness.path }}
              port: {{ $component.probes.readiness.port }}
            initialDelaySeconds: {{ $component.probes.readiness.initialDelaySeconds }}
            periodSeconds: {{ $component.probes.readiness.periodSeconds }}
            timeoutSeconds: {{ $component.probes.readiness.timeoutSeconds }}
            failureThreshold: {{ $component.probes.readiness.failureThreshold }}
          {{- end }}
          volumeMounts:
            - name: tmp
              mountPath: /tmp
            {{- if and $isMarketplace $root.Values.config.enabled }}
            - name: config
              mountPath: {{ $root.Values.config.mountPath }}
              subPath: config.yaml
              readOnly: true
            {{- end }}
            {{- if and $isMarketplace $root.Values.persistence.enabled }}
            - name: storage
              mountPath: {{ $root.Values.persistence.mountPath }}
            {{- end }}
            {{- with $root.Values.extraVolumeMounts }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
      volumes:
        - name: tmp
          emptyDir: {}
        {{- if and $isMarketplace $root.Values.config.enabled }}
        - name: config
          configMap:
            name: {{ include "agent-marketplace.configMapName" $root }}
        {{- end }}
        {{- if and $isMarketplace $root.Values.persistence.enabled }}
        - name: storage
          persistentVolumeClaim:
            claimName: {{ default (include "agent-marketplace.fullname" $root) $root.Values.persistence.existingClaim }}
        {{- end }}
        {{- with $root.Values.extraVolumes }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
{{- end }}
