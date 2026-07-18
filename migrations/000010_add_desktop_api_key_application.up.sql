alter table api_keys drop constraint api_keys_application_check;
alter table api_keys
    add constraint api_keys_application_check
    check (application in ('kvideo', 'latestnews', 'synchub-desktop'));
