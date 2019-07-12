#!/usr/local/bin/python3.7
""" Outputs a report on all EC2 instances public domain names and
public IP addresses """

import boto3
import sys

ofp = open("public.csv", "w")
session = boto3.Session()
profiles = session.available_profiles
ec2 = session.client("ec2")
regions = [R["RegionName"] for R in ec2.describe_regions()["Regions"]]

for profile in profiles:
    for region in regions:
        session = boto3.Session(profile_name=profile, region_name=region)
        ec2 = session.client("ec2")
        resp = ec2.describe_instances()
        resv = resp["Reservations"]

        for rr in resv:
            for i in rr["Instances"]:
                dns = i["PublicDnsName"].strip()
                print(dns, file=ofp)
                if "PublicIpAddress" in i:
                    ipa = i["PublicIpAddress"]
                    if type(ipa) is list:
                        for j in ipa:
                            print(j.strip(), file=ofp)
                    else:
                        print(ipa.strip(), file=ofp)
