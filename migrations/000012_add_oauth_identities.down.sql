drop table if exists oauth_login_codes;
drop table if exists oauth_identities;

update users set password_hash = '' where password_hash is null;
alter table users alter column password_hash set not null;
