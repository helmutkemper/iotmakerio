// web_pages.c — IoTMaker device: maker-composed web pages.
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// TWO blocks, one tiny server:
//
//   Register page  — endpoint + html (+ optional image). The maker feeds
//                    it with a ConstString ("home", "/home" or "/" for
//                    the site root), a Data · Text device (the html,
//                    written in Monaco) and a Data · File device (the
//                    image). Wire several instances to publish several
//                    pages.
//   Server start   — the port. Serves every registered page at its
//                    endpoint; the page's image (when wired) answers ANY
//                    path under it, so the html may reference
//                    <img src="/home/logo.png"> — or any name at all.
//                    GET / lists the pages, unless a page claims "/".
//
// Execution order: Register defaults to 1 and Server to 2 (directive
// defaults, maker-overridable) — registrations always land before the
// server blocks the loop.
//
// Português: DOIS blocos, um servidor pequeno. "Register page" recebe
// endpoint + html (+ imagem opcional) — o maker liga um ConstString, um
// Data · Text (o html escrito no Monaco) e um Data · File (a imagem);
// várias instâncias publicam várias páginas. "Server start" recebe a
// porta e serve cada página no seu endpoint; a imagem (quando ligada)
// sai em "<endpoint>/img". GET / lista as páginas. Ordem: registro
// nasce 1 e servidor nasce 2 — registros sempre antes do servidor.

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <signal.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>

/* ── Page registry ─────────────────────────────────────────────────────────
 * Fixed table, no malloc — the IoTMaker way. Eight pages is plenty for a
 * maker portal; raising the cap is one number.
 * Português: Tabela fixa, sem malloc. Oito páginas bastam para um portal
 * de maker; subir o teto é um número. */
#define WEB_MAX_PAGES 8

#define WEB_MAX_ENDPOINT 64

typedef struct {
    char                 endpoint[WEB_MAX_ENDPOINT]; /* normalized: '/'-prefixed */
    const unsigned char *html;       /* page body (UTF-8)               */
    unsigned long        html_len;
    const unsigned char *image;      /* optional: NULL when unwired     */
    unsigned long        image_len;
} web_page_t;

static web_page_t web_pages[WEB_MAX_PAGES];
static int        web_page_count = 0;

// Registers ONE page. Wire the endpoint, the html and (optionally) an
// image; each instance of this block publishes one page. The image, when
// present, is served at "<endpoint>/img".
//
// Português: Registra UMA página. Ligue o endpoint, o html e
// (opcionalmente) uma imagem; cada instância publica uma página. A
// imagem, quando presente, sai em "<endpoint>/img".
//
// label:Register page.
// icon:file-circle-plus.
// min-target:posix.
// executionOrder:1.
void web_register_page(
    // The URL path — "home" and "/home" are both accepted (the missing
    // slash is added for you). Português: O caminho da URL — "home" e
    // "/home" são aceitos (a barra que faltar é adicionada).
    // connection:mandatory.
    // doc:URL path of the page ("home" or "/home").
    const char *endpoint,
    // The page body — wire a Data · Text device and write the html in
    // Monaco. Português: O corpo da página — ligue um Data · Text.
    // connection:mandatory.
    // slice:html_len.
    // doc:Page body (UTF-8 html).
    const uint8_t *html,
    unsigned long html_len,
    // Optional image, served at "<endpoint>/img" — wire a Data · File
    // device, or leave unwired for a text-only page.
    // Português: Imagem opcional, servida em "<endpoint>/img" — ligue um
    // Data · File, ou deixe sem fio para página só-texto.
    // slice:image_len.
    // doc:Optional image (served at endpoint/img).
    const uint8_t *image,
    unsigned long image_len
) {
    if (web_page_count >= WEB_MAX_PAGES) {
        fprintf(stderr, "[web] page table full (%d) — '%s' ignored\n",
                WEB_MAX_PAGES, endpoint ? endpoint : "?");
        return;
    }
    if (endpoint == NULL || endpoint[0] == '\0' || html == NULL) {
        fprintf(stderr, "[web] invalid registration ignored\n");
        return;
    }

    /* Forgiveness over rejection: a maker typing "home" means "/home" —
     * normalize into the slot's own buffer instead of bouncing the page
     * to a stderr nobody reads (field report 2026-07-13: an empty index
     * and a silent stderr are a terrible error message).
     * Português: Perdão em vez de rejeição: maker que digita "home" quer
     * dizer "/home" — normaliza no buffer do slot em vez de derrubar a
     * página para um stderr que ninguém lê (report de campo 2026-07-13:
     * índice vazio e stderr mudo são uma péssima mensagem de erro). */
    {
        const char *fmt = (endpoint[0] == '/') ? "%s" : "/%s";
        int n = snprintf(web_pages[web_page_count].endpoint,
                         WEB_MAX_ENDPOINT, fmt, endpoint);
        if (n >= WEB_MAX_ENDPOINT) {
            fprintf(stderr, "[web] endpoint truncated: %s\n",
                    web_pages[web_page_count].endpoint);
        }
    }
    web_pages[web_page_count].html      = html;
    web_pages[web_page_count].html_len  = html_len;
    web_pages[web_page_count].image     = image;     /* NULL when unwired */
    web_pages[web_page_count].image_len = image_len; /* 0 when unwired    */
    web_page_count++;
    printf("[web] registered %s (%lu bytes html, %lu bytes image)\n",
           endpoint, html_len, image_len);
}

/* ── The server ────────────────────────────────────────────────────────────
 * Every hard-won scar from the reference portal device is here: SIGPIPE
 * ignored (an aborting browser must not kill the process), the request
 * read loop (browser headers exceed one read), send_all (sockets
 * short-write) and the lingering close (FIN, never RST).
 * Português: Toda cicatriz do device portal de referência está aqui:
 * SIGPIPE ignorado, loop de leitura do request, send_all e fechamento
 * com drenagem. */

static void web_send_all(int fd, const unsigned char *buf, unsigned long len) {
    unsigned long off = 0;
    while (off < len) {
        long n = (long)write(fd, buf + off, (size_t)(len - off));
        if (n <= 0) {
            return;
        }
        off += (unsigned long)n;
    }
}

static void web_send(int fd, const char *status, const char *ctype,
                     const unsigned char *body, unsigned long len) {
    char head[256];
    int n = snprintf(head, sizeof(head),
                     "HTTP/1.1 %s\r\n"
                     "Content-Type: %s\r\n"
                     "Content-Length: %lu\r\n"
                     "Connection: close\r\n\r\n",
                     status, ctype, len);
    if (n > 0) {
        web_send_all(fd, (const unsigned char *)head, (unsigned long)n);
    }
    if (body != NULL && len > 0) {
        web_send_all(fd, body, len);
    }
}

/* Sniffs the image Content-Type from magic bytes — good enough for the
 * whitelist formats. Português: Deduz o Content-Type pelos magic bytes. */
static const char *web_image_type(const unsigned char *img, unsigned long len) {
    if (len >= 8 && img[0] == 0x89 && img[1] == 'P') return "image/png";
    if (len >= 3 && img[0] == 'G' && img[1] == 'I')  return "image/gif";
    if (len >= 3 && img[0] == 0xFF && img[1] == 0xD8) return "image/jpeg";
    if (len >= 4 && img[0] == '<')                    return "image/svg+xml";
    return "application/octet-stream";
}

/* GET / — a tiny auto-index of every registered page.
 * Português: GET / — um índice automático das páginas registradas. */
static void web_send_index(int fd) {
    char body[1024];
    int n = snprintf(body, sizeof(body),
                     "<!doctype html><meta charset=\"utf-8\">"
                     "<title>IoTMaker pages</title>"
                     "<h1>Pages</h1><ul>");
    for (int i = 0; i < web_page_count && n < (int)sizeof(body) - 96; i++) {
        n += snprintf(body + n, sizeof(body) - (size_t)n,
                      "<li><a href=\"%s\">%s</a></li>",
                      web_pages[i].endpoint, web_pages[i].endpoint);
    }
    n += snprintf(body + n, sizeof(body) - (size_t)n, "</ul>");
    web_send(fd, "200 OK", "text/html; charset=utf-8",
             (const unsigned char *)body, (unsigned long)n);
}

// Starts the server and blocks forever, serving every registered page at
// its endpoint (and its image at "<endpoint>/img"). Wire the port from a
// ConstInt. Register pages BEFORE this block runs — the default execution
// order (register=1, server=2) already guarantees it.
//
// Português: Sobe o servidor e bloqueia para sempre, servindo cada página
// registrada no seu endpoint (e a imagem em "<endpoint>/img"). Ligue a
// porta de um ConstInt. Registre as páginas ANTES deste bloco — a ordem
// default (registro=1, servidor=2) já garante.
//
// label:Server start.
// icon:server.
// min-target:posix.
// executionOrder:2.
void web_server_start(
    // connection:mandatory.
    // doc:TCP port to listen on (e.g. 9000).
    int port
) {
    signal(SIGPIPE, SIG_IGN);

    int srv = socket(AF_INET, SOCK_STREAM, 0);
    if (srv < 0) {
        fprintf(stderr, "[web] socket() failed\n");
        return;
    }
    int yes = 1;
    setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family      = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port        = htons((unsigned short)port);
    if (bind(srv, (struct sockaddr *)&addr, sizeof(addr)) < 0 ||
        listen(srv, 8) < 0) {
        fprintf(stderr, "[web] bind/listen on %d failed\n", port);
        close(srv);
        return;
    }
    printf("[web] serving %d page(s) on http://localhost:%d/\n",
           web_page_count, port);

    for (;;) {
        int cli = accept(srv, NULL, NULL);
        if (cli < 0) {
            continue;
        }

        /* Read until the end of the request headers (browsers exceed one
         * read). Português: Lê até o fim dos headers. */
        char req[2048];
        long got = 0;
        while (got < (long)sizeof(req) - 1) {
            long n = (long)read(cli, req + got, sizeof(req) - 1 - (size_t)got);
            if (n <= 0) {
                break;
            }
            got += n;
            req[got] = '\0';
            if (strstr(req, "\r\n\r\n") != NULL) {
                break;
            }
        }
        if (got <= 0) {
            close(cli);
            continue;
        }

        /* Parse "GET <path> " — path bounded by the two spaces.
         * Português: Extrai o path entre os dois espaços. */
        char path[256] = "/";
        if (strncmp(req, "GET ", 4) == 0) {
            const char *p   = req + 4;
            const char *end = strchr(p, ' ');
            size_t plen = end ? (size_t)(end - p) : 0;
            if (plen > 0 && plen < sizeof(path)) {
                memcpy(path, p, plen);
                path[plen] = '\0';
            }
        }

        /* Routing, two passes — forgiveness by design:
         *
         *   Pass 1: EXACT endpoint match serves the page. A page
         *           registered at "/" IS the site root; the auto-index
         *           only exists while nobody claims "/".
         *   Pass 2: the page's image answers ANY path under its
         *           endpoint — the maker writes <img src="/home/logo.png">
         *           or "/home/img" or whatever feels natural, and it just
         *           works (one image per page keeps this unambiguous;
         *           field report 2026-07-13: a fixed "/img" convention is
         *           knowledge the maker doesn't have).
         *
         * Português: Roteamento em dois passes — perdão por design.
         * Passe 1: match EXATO serve a página; página registrada em "/"
         * É a raiz do site (o índice automático só existe enquanto
         * ninguém reivindica "/"). Passe 2: a imagem da página responde
         * QUALQUER caminho sob o endpoint — o maker escreve o src que
         * lhe parecer natural e funciona (uma imagem por página mantém
         * isso sem ambiguidade; a convenção fixa "/img" era conhecimento
         * que o maker não tem). */
        int served = 0;

        for (int i = 0; i < web_page_count && !served; i++) {
            if (strcmp(path, web_pages[i].endpoint) == 0) {
                web_send(cli, "200 OK", "text/html; charset=utf-8",
                         web_pages[i].html, web_pages[i].html_len);
                served = 1;
            }
        }

        if (!served && strcmp(path, "/") == 0) {
            web_send_index(cli);
            served = 1;
        }

        for (int i = 0; i < web_page_count && !served; i++) {
            const web_page_t *pg = &web_pages[i];
            size_t elen = strlen(pg->endpoint);
            /* Under "/home" means "/home/..."; under "/" means any
             * leftover path. Português: Sob "/home" é "/home/..."; sob
             * "/" é qualquer caminho restante. */
            int under = (strcmp(pg->endpoint, "/") == 0)
                            ? 1
                            : (strncmp(path, pg->endpoint, elen) == 0 &&
                               path[elen] == '/');
            if (under && pg->image != NULL && pg->image_len > 0) {
                web_send(cli, "200 OK",
                         web_image_type(pg->image, pg->image_len),
                         pg->image, pg->image_len);
                served = 1;
            }
        }

        if (!served) {
            static const unsigned char nf[] = "not found";
            web_send(cli, "404 Not Found", "text/plain",
                     nf, (unsigned long)(sizeof(nf) - 1));
        }

        /* Lingering close: FIN after the full response, never RST.
         * Português: Fecha com drenagem — FIN, nunca RST. */
        shutdown(cli, SHUT_WR);
        {
            char sink[512];
            int budget = 16;
            while (budget-- > 0 && read(cli, sink, sizeof(sink)) > 0) {
            }
        }
        close(cli);
    }
}
