FROM debian

ARG USERNAME=ipspinner
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN apt update
RUN apt install -y build-essential curl wget

# Install go
RUN wget https://go.dev/dl/go1.22.1.linux-amd64.tar.gz
RUN tar -C /usr/local -xzf go1.22.1.linux-amd64.tar.gz
RUN rm -f go1.22.1.linux-amd64.tar.gz
ENV PATH="$PATH:/usr/local/go/bin"

COPY . /go/workspace/ipspinner
WORKDIR /go/workspace/ipspinner

RUN printf "go 1.22.1\n\nuse ./ipspinner" > /go/workspace/go.work

RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME

RUN chown -R $USERNAME /go/workspace/ipspinner

USER $USERNAME
WORKDIR /go/workspace/ipspinner

RUN go mod tidy
RUN make build-linux

ENTRYPOINT ["/go/workspace/ipspinner/ipspinner"]
