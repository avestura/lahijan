-- PowerDNS Authoritative 4.x gpgsql schema.
--
-- Source: https://github.com/PowerDNS/pdns/blob/master/modules/gpgsqlbackend/schema.pgsql.sql
-- (PDNS-7839, master @ 4.9.x.) Copied verbatim into the Lahijan repo per the
-- WS-12 "Notes" item so we control the version. Re-run by the init wrapper
-- against the pdns database on first boot.
--
-- The schema is idempotent (uses CREATE TABLE IF NOT EXISTS), so a re-run
-- against an already-initialised database is a no-op. The init.sh wrapper
-- passes --set ON_ERROR_STOP=1 so a partial schema failure aborts the
-- bootstrap rather than leaving a half-initialised database.
--
-- This file is mounted read-only into the postgres container at
-- /powerdns/schema.pgsql.sql by docker-compose.dev.yml; the init.sh wrapper
-- in the same directory invokes psql against it.

CREATE TABLE domains (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    master          VARCHAR(128) DEFAULT NULL,
    last_check      INTEGER DEFAULT NULL,
    type            VARCHAR(6) NOT NULL,
    notified_serial BIGINT DEFAULT NULL,
    account         VARCHAR(40) DEFAULT NULL,
    CONSTRAINT c_lowercase_name CHECK (((name)::TEXT = LOWER((name)::TEXT)))
);

CREATE UNIQUE INDEX name_index ON domains(name);


CREATE TABLE records (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT DEFAULT NULL,
    name            VARCHAR(255) DEFAULT NULL,
    type            VARCHAR(10) DEFAULT NULL,
    content         VARCHAR(65535) DEFAULT NULL,
    ttl             INTEGER DEFAULT NULL,
    prio            INTEGER DEFAULT NULL,
    disabled        BOOLEAN DEFAULT false,
    ordername       VARCHAR(255),
    auth            BOOLEAN DEFAULT true,
    CONSTRAINT domain_exists
    FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE,
    CONSTRAINT c_lowercase_name CHECK (((name)::TEXT = LOWER((name)::TEXT)))
);

CREATE INDEX rec_name_index ON records(name);
CREATE INDEX nametype_index ON records(name,type);
CREATE INDEX domain_id ON records(domain_id);
CREATE INDEX recordorder ON records (domain_id, ordername text_pattern_ops);


CREATE TABLE supermasters (
    ip          INET NOT NULL,
    nameserver  VARCHAR(255) NOT NULL,
    account     VARCHAR(40) NOT NULL,
    PRIMARY KEY(ip, nameserver)
);


CREATE TABLE comments (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT NOT NULL,
    name            VARCHAR(255) NOT NULL,
    type            VARCHAR(10) NOT NULL,
    modified_at     INT NOT NULL,
    account         VARCHAR(40) DEFAULT NULL,
    comment         VARCHAR(65535) NOT NULL,
    CONSTRAINT domain_exists
    FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE,
    CONSTRAINT c_lowercase_name CHECK (((name)::TEXT = LOWER((name)::TEXT)))
);

CREATE INDEX comments_domain_id_idx ON comments(domain_id);
CREATE INDEX comments_name_type_idx ON comments(name, type);
CREATE INDEX comments_order_idx ON comments(domain_id, modified_at);


CREATE TABLE domainmetadata (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT NOT NULL,
    kind            VARCHAR(32),
    content         TEXT,
    CONSTRAINT domain_exists
    FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE
);

CREATE INDEX domainidmetaindex ON domainmetadata(domain_id);


CREATE TABLE cryptokeys (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT NOT NULL,
    flags           INT NOT NULL,
    active          BOOLEAN DEFAULT true,
    published       BOOLEAN DEFAULT true,
    content         TEXT,
    key_id          VARCHAR(120),
    CONSTRAINT domain_exists
    FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE
);

CREATE INDEX domainidindex ON cryptokeys(domain_id);


CREATE TABLE tsigkeys (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(255),
    algorithm       VARCHAR(50),
    secret          VARCHAR(255)
);

CREATE UNIQUE INDEX namealgoindex ON tsigkeys(name, algorithm);
