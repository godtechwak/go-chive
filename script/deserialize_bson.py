import os
from pymongo import MongoClient
from bson import decode_all

"""
    bson 포맷의 데이터를 역직렬화하여 MongoDB 컬렉션에 삽입한다. 즉 아카이빙 되기 전 상태로 복원한다.
"""
client = MongoClient("mongodb+srv://<USER>:<PASSWORD>@<mongodb uri>/?tls=false")
db = client["<database name>"]
collection = db["<collection name>"]

current_dir = os.getcwd()
bson_files = [f for f in os.listdir(current_dir) if f.endswith(".bson") or f.endswith(".json")]

total_inserted = 0

for filename in bson_files:
    path = os.path.join(current_dir, filename)
    print(f"- Processing file: {filename}")

    try:
        with open(path, "rb") as f:
            raw_data = f.read()

        docs_wrapper = decode_all(raw_data)[0]
        docs = docs_wrapper.get("docs", [])

        if not docs:
            print(f"!!!! No documents in {filename}")
            continue

        # 중복 _id 확인
        all_ids = [doc["_id"] for doc in docs if "_id" in doc]
        existing_ids = set(doc["_id"] for doc in collection.find({"_id": {"$in": all_ids}}, {"_id": 1}))
        docs_to_insert = [doc for doc in docs if doc["_id"] not in existing_ids]

        if docs_to_insert:
            result = collection.insert_many(docs_to_insert)
            inserted_count = len(result.inserted_ids)
            total_inserted += inserted_count
            print(f"(v) Inserted {inserted_count} new documents from {filename}")
        else:
            print(f"(i) All documents in {filename} already exist.")

    except Exception as e:
        print(f"❌ Error processing {filename}: {e}")

print(f"\n🎉 Done. Total inserted documents: {total_inserted}")
