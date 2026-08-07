#!/usr/bin/env python3
"""Sanctions Screening API — Python client example.

Subscribe on RapidAPI to get keys:
https://rapidapi.com/CooLCHI-gun/api/sanctions-screening-api
"""
import os
import requests

RAPIDAPI_KEY = os.environ["RAPIDAPI_KEY"]
TENANT_KEY = os.environ["SANCTIONS_TENANT_KEY"]
HOST = "sanctions-screening-api2.p.rapidapi.com"


def screen(query_name, entity_type="individual", threshold=0.7):
    resp = requests.post(
        f"https://{HOST}/screen",
        json={
            "query_name": query_name,
            "entity_type": entity_type,
            "threshold": threshold,
        },
        headers={
            "Content-Type": "application/json",
            "X-RapidAPI-Key": RAPIDAPI_KEY,
            "X-RapidAPI-Host": HOST,
            "X-API-Key": TENANT_KEY,
        },
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


if __name__ == "__main__":
    result = screen("Kim Jong Un")
    print(f"status: {result['status']}")
    print(f"matches: {result['total_matches']}")
    print(f"data_version: {result['data_version']}")
    print(f"sources: {result['sources_covered']}")
    for c in result["candidates"]:
        print(f"  - {c['entity_name']} conf={c['confidence_score']:.2f} list={c['list_name']}")
