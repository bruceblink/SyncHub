delete from app_metadata_documents
where application = 'latestnews' and collection = 'preferences';

alter table app_metadata_documents
    drop constraint app_metadata_documents_collection_check;

alter table app_metadata_documents
    add constraint app_metadata_documents_collection_check
    check (collection in ('watch-history', 'favorites', 'reading-history'));
