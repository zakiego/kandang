# The sandbox image. Kandang never runs it with Docker: deploy/deploy.sh builds
# it, exports a container's filesystem to a tar and unpacks that as the
# read-only rootfs every gVisor run shares.
#
# Python libs from Alpine's packages where they exist (prebuilt, no compiler
# needed), pip only for the three that aren't packaged. The matplotlib font
# cache is built once at /opt/mpl so a chart doesn't spend seconds on it in
# every run. uid and gid 10001 match the runner (internal/sandbox).
#
# Charts get a house style without any code: Inter (static TTFs, OFL, in
# fonts/), the rc file at /opt/mpl/matplotlibrc (the runner copies /opt/mpl
# into every run) and sitecustomize.py for the defaults an rc file can't hold.
FROM alpine:3.24

RUN apk add --no-cache \
      bash coreutils python3 \
      py3-numpy py3-pandas py3-matplotlib py3-pillow py3-requests py3-pip \
 && pip install --no-cache-dir --no-compile --break-system-packages openpyxl pypdf python-docx \
 && apk del py3-pip \
 && find /usr/lib/python3* -type d \( -name tests -o -name __pycache__ \) -prune -exec rm -rf {} + \
 && rm -rf /usr/lib/python3*/site-packages/matplotlib/mpl-data/sample_data /root/.cache

COPY fonts/ /usr/share/fonts/inter/
COPY matplotlibrc /opt/mpl/matplotlibrc
COPY sitecustomize.py /tmp/sitecustomize.py

RUN mv /tmp/sitecustomize.py "$(python3 -c 'import sysconfig; print(sysconfig.get_path("purelib"))')/sitecustomize.py" \
 && MPLCONFIGDIR=/opt/mpl python3 -c 'import matplotlib.font_manager' \
 && chmod -R a+rX /opt/mpl /usr/share/fonts/inter \
 && addgroup -g 10001 sandbox \
 && adduser -D -u 10001 -G sandbox -h /tmp -s /bin/bash sandbox \
 && mkdir -p /work

ENV PYTHONDONTWRITEBYTECODE=1 MPLCONFIGDIR=/opt/mpl HOME=/tmp
USER 10001:10001
WORKDIR /work
