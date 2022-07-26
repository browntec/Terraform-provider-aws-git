---
subcategory: "SSM (Systems Manager)"
layout: "aws"
page_title: "AWS: aws_ssm_parameter"
description: |-
  Provides a SSM Parameter resource
---

# Resource: aws_ssm_parameter

Provides an SSM Parameter resource.

## Example Usage

To store a basic string parameter:

```terraform
resource "aws_ssm_parameter" "foo" {
  name  = "foo"
  type  = "String"
  value = "bar"
}
```

To store an encrypted string using the default SSM KMS key:

```terraform
resource "aws_db_instance" "default" {
  allocated_storage    = 10
  storage_type         = "gp2"
  engine               = "mysql"
  engine_version       = "5.7.16"
  instance_class       = "db.t2.micro"
  name                 = "mydb"
  username             = "foo"
  password             = var.database_master_password
  db_subnet_group_name = "my_database_subnet_group"
  parameter_group_name = "default.mysql5.7"
}

resource "aws_ssm_parameter" "secret" {
  name        = "/production/database/password/master"
  description = "The parameter description"
  type        = "SecureString"
  value       = var.database_master_password

  tags = {
    environment = "production"
  }
}
```

~> **Note:** The unencrypted value of a SecureString will be stored in the raw state as plain-text.
[Read more about sensitive data in state](https://www.terraform.io/docs/state/sensitive-data.html).

## Argument Reference

The following arguments are supported:

* `name` - (Required) The name of the parameter. If the name contains a path (e.g., any forward slashes (`/`)), it must be fully qualified with a leading forward slash (`/`). For additional requirements and constraints, see the [AWS SSM User Guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/sysman-parameter-name-constraints.html).
* `type` - (Required) The type of the parameter. Valid types are `String`, `StringList` and `SecureString`.
* `value` - (Required) The value of the parameter. This value is always marked as sensitive in the Terraform plan output, regardless of `type`. In Terraform CLI version 0.15 and later, this may require additional configuration handling for certain scenarios. For more information, see the [Terraform v0.15 Upgrade Guide](https://www.terraform.io/upgrade-guides/0-15.html#sensitive-output-values).
* `description` - (Optional) The description of the parameter.
* `tier` - (Optional) The parameter tier to assign to the parameter.
  If not specified, will use the default parameter tier for the region.
  Valid tiers are `Standard`, `Advanced`, and `Intelligent-Tiering`.
  Downgrading an `Advanced` tier parameter to `Standard` will recreate the resource.
  For more information on parameter tiers, see the [AWS SSM Parameter tier comparison and guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/parameter-store-advanced-parameters.html).
* `key_id` - (Optional) The KMS key id or arn for encrypting a SecureString.
* `overwrite` - (Optional) Overwrite an existing parameter. If not specified, will default to `false` if the resource has not been created by terraform to avoid overwrite of existing resource and will default to `true` otherwise (terraform lifecycle rules should then be used to manage the update behavior).
* `allowed_pattern` - (Optional) A regular expression used to validate the parameter value.
* `data_type` - (Optional) The data_type of the parameter. Valid values: text and aws:ec2:image for AMI format, see the [Native parameter support for Amazon Machine Image IDs
](https://docs.aws.amazon.com/systems-manager/latest/userguide/parameter-store-ec2-aliases.html)
* `tags` - (Optional) A map of tags to assign to the object. If configured with a provider [`default_tags` configuration block](/docs/providers/aws/index.html#default_tags-configuration-block) present, tags with matching keys will overwrite those defined at the provider-level.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

* `arn` - The ARN of the parameter.
* `name` - (Required) The name of the parameter.
* `description` - (Required) The description of the parameter.
* `tags_all` - A map of tags assigned to the resource, including those inherited from the provider [`default_tags` configuration block](/docs/providers/aws/index.html#default_tags-configuration-block).
* `type` - (Required) The type of the parameter. Valid types are `String`, `StringList` and `SecureString`.
* `value` - (Required) The value of the parameter.
* `version` - The version of the parameter.

## Import

SSM Parameters can be imported using the `parameter store name`, e.g.,

```
$ terraform import aws_ssm_parameter.my_param /my_path/my_paramname
```
