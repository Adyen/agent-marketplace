ARG PYTHON_IMAGE=python:3.12-slim

FROM ${PYTHON_IMAGE} AS build
USER 0:0
WORKDIR /wheelhouse
COPY wheelhouse/ /wheelhouse/
RUN --network=none set -eu; \
    printf '%s  %s\n' \
    'e7b46c55e726405b2a068d375d1ee8e799e15d86f3fc05f84feb5ba962198b15' \
    'skillspector-2.13.6-py3-none-any.whl' | sha256sum -c - \
    && awk ' \
      /^[[:space:]]*(#|$)/ { print; next } \
      { \
        requirement = $0; \
        sub(/^[[:space:]]*/, "", requirement); \
        name = requirement; \
        sub(/[^[:alnum:]_.-].*$/, "", name); \
        name = tolower(name); \
        gsub(/[-_.]/, "-", name); \
        if (name == "skillspector") { \
          count++; \
          if (tolower($0) !~ /^[[:space:]]*skillspector==[^[:space:]]+[[:space:]]+--hash=sha256:[[:xdigit:]]+([[:space:]]+#.*)?$/) invalid = 1; \
          print "skillspector @ file:///wheelhouse/skillspector-2.13.6-py3-none-any.whl --hash=sha256:e7b46c55e726405b2a068d375d1ee8e799e15d86f3fc05f84feb5ba962198b15"; \
        } else { \
          print; \
        } \
      } \
      END { if (count != 1 || invalid) exit 1 } \
    ' /wheelhouse/requirements.lock > /wheelhouse/requirements.pinned.lock \
    && python -m venv /opt/scanner \
    && /opt/scanner/bin/python -m pip install --no-index --only-binary=:all: --require-hashes --find-links=/wheelhouse -r /wheelhouse/requirements.pinned.lock \
    && /opt/scanner/bin/python -m pip check \
    && /opt/scanner/bin/python -c 'from importlib.metadata import version; assert version("skillspector") == "2.13.6"'

FROM ${PYTHON_IMAGE}
USER 0:0
COPY --from=build /opt/scanner /opt/scanner
COPY runner.py /app/runner.py
RUN mkdir -p /run/skillspector && chown 1000:1000 /run/skillspector
USER 1000:1000
ENTRYPOINT ["/opt/scanner/bin/python", "/app/runner.py"]
