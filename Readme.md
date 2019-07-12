# Summary

What you have here are three programs which, together, generate an 
inventory of AWS Route 53 domain names, classifying some as invalid and 
others as potentially invalid. 

Validity is tested by looking for domain names which are aliases to 
EC2s or ELBs which are not running in any of our accounts. This is 
designed assuming all the domain names are in a single AWS account.
It is easily extendable to look at domain names from several accounts.

The main program is route53.go. See that program for further documentation
on the suite. 
