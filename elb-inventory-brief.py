#!/usr/local/bin/python3.7
""" Generate a report of elastic load balancers in all accounts """
import boto3
import sys

ofp = open("elbs.txt", "w")
session = boto3.Session()
profiles = session.available_profiles
ec2 = session.client("ec2")
regions = [R["RegionName"] for R in ec2.describe_regions()["Regions"]]

for profile in profiles:
    for region in regions:
        print(f"{profile} :: {region}")
        session = boto3.Session(profile_name=profile, region_name=region)
        elbv2 = session.client("elbv2")
        next_marker = "first start"
        resp = elbv2.describe_load_balancers()
        while next_marker is not None:
            for lb in resp["LoadBalancers"]:
                print(lb["DNSName"], ofp)
            if "NextMarker" in resp:
                next_marker = resp["NextMarker"]
                resp = elbv2.describe_load_balancers(Marker=next_marker)
            else:
                next_marker = None

        elb = session.client("elb")
        next_marker = "first start"
        resp = elb.describe_load_balancers()
        while next_marker is not None:
            for lb in resp["LoadBalancerDescriptions"]:
                print(lb["DNSName"], file=ofp)
            if "NextMarker" in resp:
                next_marker = resp["NextMarker"]
                resp = elb.describe_load_balancers(Marker=next_marker)
            else:
                next_marker = None
