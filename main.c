/* SPDX-License-Identifier: Apache-2.0 */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <linux/seccomp.h>
#include <seccomp.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/uio.h>
#include <sys/un.h>
#include <unistd.h>

#include <glib.h>

/* glibc does not provide wrapper for pidfd_open() */
static int _mydef_pidfd_open(pid_t pid, unsigned int flags) {
  return syscall(__NR_pidfd_open, pid, flags);
}

#ifndef __NR_pidfd_getfd
#ifdef __x86_64__
#define __NR_pidfd_getfd 438
#else
#error "__NR_pidfd_getfd is not defined"
#endif
#endif

/* glibc does not provide wrapper for pidfd_getfd() */
static int _mydef_pidfd_getfd(int pidfd, int targetfd, unsigned int flags) {
  return syscall(__NR_pidfd_getfd, pidfd, targetfd, flags);
}

/* libseccomp does not provide wrapper for seccomp_notif_addfd */
struct _mydef_seccomp_notif_addfd {
  __u64 id;
  __u32 flags;
  __u32 srcfd;
  __u32 newfd;
  __u32 newfd_flags;
};

#ifndef SECCOMP_ADDFD_FLAG_SETFD
#define SECCOMP_ADDFD_FLAG_SETFD (1UL << 0)
#endif

#ifndef SECCOMP_IOCTL_NOTIF_ADDFD
#define SECCOMP_IOCTL_NOTIF_ADDFD                                              \
  SECCOMP_IOW(3, struct _mydef_seccomp_notif_addfd)
#endif

/*
 * recvfd() was copied from
 * https://github.com/rootless-containers/slirp4netns/blob/d5c44a94a271701ddc48c9b20aa6e9539a92ad0a/main.c#L110-L141
 * The author (Akihiro Suda) relicensed the code from GPL-2.0-or-later to
 * Apache-2.0.
 */
static int recvfd(int sock) {
  int fd;
  ssize_t rc;
  struct msghdr msg;
  struct cmsghdr *cmsg;
  char cmsgbuf[CMSG_SPACE(sizeof(fd))];
  struct iovec iov;
  char dummy = '\0';
  memset(&msg, 0, sizeof(msg));
  iov.iov_base = &dummy;
  iov.iov_len = 1;
  msg.msg_iov = &iov;
  msg.msg_iovlen = 1;
  msg.msg_control = cmsgbuf;
  msg.msg_controllen = sizeof(cmsgbuf);
  if ((rc = recvmsg(sock, &msg, 0)) < 0) {
    perror("recvmsg");
    return (int)rc;
  }
  if (rc == 0) {
    fprintf(stderr, "the message is empty\n");
    return -1;
  }
  cmsg = CMSG_FIRSTHDR(&msg);
  if (cmsg == NULL || cmsg->cmsg_type != SCM_RIGHTS) {
    fprintf(stderr, "the message does not contain fd\n");
    return -1;
  }
  memcpy(&fd, CMSG_DATA(cmsg), sizeof(fd));
  return fd;
}

static ssize_t read_proc_mem(void **out, pid_t pid, off_t off, size_t buf_len) {
  void *buf = malloc(buf_len);
  struct iovec local[1];
  struct iovec remote[1];
  ssize_t nread = -1;
  local[0].iov_base = buf;
  local[0].iov_len = buf_len;
  remote[0].iov_base = (void *)off;
  remote[0].iov_len = buf_len;
  if ((nread = process_vm_readv(pid, local, 1, remote, 1, 0)) < 0) {
    perror("process_vm_readv");
    free(buf);
    *out = NULL;
    return nread;
  }
  *out = buf;
  return nread;
}

struct ctx {
  int notify_fd;
  struct seccomp_notif *req;
  struct seccomp_notif_resp *resp;
};

static void handle_sys_connect(struct ctx *ctx) {
  int pidfd = -1, sockfd = -1;
  int sockfd_num = ctx->req->data.args[0];
  struct sockaddr *addr = NULL;
  size_t addrlen = ctx->req->data.args[2];
  read_proc_mem((void **)&addr, ctx->req->pid, ctx->req->data.args[1], addrlen);
  if (addr->sa_family != AF_INET) {
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  struct sockaddr_in *sin = (struct sockaddr_in *)(addr);
  uint16_t port = ntohs(sin->sin_port);
  uint32_t ip = ntohl(sin->sin_addr.s_addr);
  printf("connect(pid=%d): sockfd_num=%d, port=%d, ip=0x%08x\n", ctx->req->pid,
         sockfd_num, port, ip);
  switch (ip >> 24) {
  case 127:
    /*
     * Best-effort to block connecting to 127.0.0.0/8 on the host network
     * namespace.
     *
     * CAUTION: the tracee process might be able to connect to 127.0.0.0/8 on
     * the host by exploiting TOCTOU of `struct sockaddr *` pointer.
     *
     * https://elixir.bootlin.com/linux/v5.9/source/include/uapi/linux/seccomp.h#L81
     *
     */
    printf("skipping local ip=0x%08x\n", ip);
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
    break;
  case 10:
    printf("skipping (possibly) `podman network create` network ip=0x%08x\n",
           ip);
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
    break;
  case 172:
    printf("skipping (possibly) `podman network create` network ip=0x%08x\n",
           ip);
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
    break;
  default:
    break;
  }
  /* pidfd_open requires kernel >= 5.3 */
  pidfd = _mydef_pidfd_open(ctx->req->pid, 0);
  if (pidfd < 0) {
    perror("pidfd_open");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  /* pidfd_getfd requires kernel >= 5.6 */
  sockfd = _mydef_pidfd_getfd(pidfd, sockfd_num, 0);
  if (sockfd < 0) {
    perror("pidfd_getfd");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  printf("got sockfd=%d\n", sockfd);
  int sock_domain;
  socklen_t sock_domain_len = sizeof(sock_domain);
  if (getsockopt(sockfd, SOL_SOCKET, SO_DOMAIN, &sock_domain,
                 &sock_domain_len) < 0) {
    perror("getsockopt(SO_DOMAIN)");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  if (sock_domain != AF_INET) {
    fprintf(stderr, "expected AF_INET, got %d\n", sock_domain);
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  int sock_type;
  socklen_t sock_type_len = sizeof(sock_type);
  if (getsockopt(sockfd, SOL_SOCKET, SO_TYPE, &sock_type, &sock_type_len) < 0) {
    perror("getsockopt(SO_TYPE)");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  if (sock_type != SOCK_STREAM) {
    fprintf(stderr, "expected SOCK_STREAM, got %d\n", sock_type);
    ctx->resp->error = -ENOTSUP;
    goto ret;
  }
  int sock_protocol;
  socklen_t sock_protocol_len = sizeof(sock_protocol);
  if (getsockopt(sockfd, SOL_SOCKET, SO_PROTOCOL, &sock_protocol,
                 &sock_protocol_len) < 0) {
    perror("getsockopt(SO_PROTOCOL)");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }

  int sockfd2 = socket(sock_domain, sock_type, sock_protocol);
  if (sockfd2 < 0) {
    perror("socket");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }

  struct _mydef_seccomp_notif_addfd addfd = {
      .id = ctx->req->id,
      .flags = SECCOMP_ADDFD_FLAG_SETFD,
      .srcfd = sockfd2,
      .newfd = sockfd_num,
      .newfd_flags = 0,
  };
  if (ioctl(ctx->notify_fd, SECCOMP_IOCTL_NOTIF_ADDFD, &addfd) < 0) {
    perror("ioctl(SECCOMP_IOCTL_NOTIF_ADDFD)");
    ctx->resp->flags |= SECCOMP_USER_NOTIF_FLAG_CONTINUE;
    goto ret;
  }
  printf("ioctl successful\n");

  int rc = connect(sockfd2, addr, addrlen);
  if (rc == 0) {
    printf("connect(pid=%d): called connect() with sockfd=%d, rc=%d\n",
           ctx->req->pid, sockfd, rc);
  } else {
    printf(
        "connect(pid=%d): called connect() with sockfd=%d, rc=%d, errno=%s\n",
        ctx->req->pid, sockfd, rc, strerror(errno));
  }
  ctx->resp->val = rc;
  ctx->resp->error = rc == 0 ? 0 : -errno;
ret:
  if (pidfd >= 0) {
    close(pidfd);
  }
  if (sockfd >= 0) {
    close(sockfd);
  }
  if (addr != NULL) {
    free(addr);
  }
  return;
}

static void handle_req(struct ctx *ctx) {
  ctx->resp->id = ctx->req->id;
  switch (ctx->req->data.nr) {
  /* FIXME: use SCMP_SYS macro */
  case __NR_connect:
    handle_sys_connect(ctx);
    break;
  default:
    fprintf(stderr, "Unexpected syscall %d, returning -ENOTSUP\n",
            ctx->req->data.nr);
    ctx->resp->error = -ENOTSUP;
    break;
  }
}

static int on_accept(int accept_fd) {
  int notify_fd = -1;
  if ((notify_fd = recvfd(accept_fd)) < 0) {
    perror("recvfd");
    return notify_fd;
  }
  printf("received notify_fd=%d\n", notify_fd);
  for (;;) {
    int rc = -1;
    struct seccomp_notif *req = NULL;
    struct seccomp_notif_resp *resp = NULL;
    if ((rc = seccomp_notify_alloc(&req, &resp)) < 0) {
      fprintf(stderr, "seccomp_notify_alloc() failed, rc=%d\n", rc);
      return rc;
    }
    if ((rc = seccomp_notify_receive(notify_fd, req)) < 0) {
      fprintf(stderr, "seccomp_notify_receive() failed, rc=%d\n", rc);
      seccomp_notify_free(req, resp);
      return rc;
    }
    if ((rc = seccomp_notify_id_valid(notify_fd, req->id)) < 0) {
      fprintf(stderr, "req->id=%lld is no longer valid, ignoring\n", req->id);
      seccomp_notify_free(req, resp);
      continue;
    }
    struct ctx ctx = {
        .notify_fd = notify_fd,
        .req = req,
        .resp = resp,
    };
    handle_req(&ctx);
    if ((rc = seccomp_notify_respond(notify_fd, resp)) < 0) {
      fprintf(stderr, "seccomp_notify_respond() failed, rc=%d\n", rc);
      seccomp_notify_free(req, resp);
      return rc;
    }
    seccomp_notify_free(req, resp);
  }
}

int main(int argc, char *const argv[]) {
  const char *xdg_runtime_dir = getenv("XDG_RUNTIME_DIR");
  const char *sock_path = NULL;
  int sock_fd = -1;
  const int sock_backlog = 128;
  struct sockaddr_un sun;
  if (xdg_runtime_dir == NULL) {
    fprintf(stderr, "XDG_RUNTIME_DIR is unset\n");
    exit(EXIT_FAILURE);
  }
  sock_path = g_strdup_printf("%s/bypass4netns.sock", xdg_runtime_dir);
  unlink(sock_path); /* remove existing socket */
  if ((sock_fd = socket(AF_UNIX, SOCK_STREAM, 0)) < 0) {
    perror("socket");
    exit(EXIT_FAILURE);
  }
  memset(&sun, 0, sizeof(struct sockaddr_un));
  sun.sun_family = AF_UNIX;
  strncpy(sun.sun_path, sock_path, sizeof(sun.sun_path) - 1);
  if (bind(sock_fd, (struct sockaddr *)&sun, sizeof(sun)) < 0) {
    perror("bind");
    exit(EXIT_FAILURE);
  }
  if (listen(sock_fd, sock_backlog) < 0) {
    perror("listen");
    exit(EXIT_FAILURE);
  }
  printf("Listening on %s\n", sock_path);
  for (;;) {
    int accept_fd = -1;
    if ((accept_fd = accept(sock_fd, NULL, NULL)) < 0) {
      perror("accept");
      exit(EXIT_FAILURE);
    }
    pid_t pid = fork();
    if (pid > 0) {
      close(accept_fd);
    } else if (pid == 0) {
      if (!on_accept(accept_fd)) {
        fprintf(stderr, "on_accept() failed\n");
        exit(EXIT_FAILURE);
      }
    } else {
      perror("fork");
      exit(EXIT_FAILURE);
    }
  }
  exit(EXIT_SUCCESS);
}
