package ec2_test

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/hashicorp/aws-sdk-go-base/tfawserr"
	multierror "github.com/hashicorp/go-multierror"
	sdkacctest "github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/acctest"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/provider"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
)

// This will currently skip EIPs with associations,
// although we depend on aws_vpc to potentially have
// the majority of those associations removed.
func init() {
	resource.AddTestSweepers("aws_eip", &resource.Sweeper{
		Name: "aws_eip",
		Dependencies: []string{
			"aws_vpc",
		},
		F: testSweepEC2Eips,
	})
}

func testSweepEC2Eips(region string) error {
	client, err := acctest.SharedRegionalSweeperClient(region)

	if err != nil {
		return fmt.Errorf("error getting client: %s", err)
	}

	conn := client.(*conns.AWSClient).EC2Conn

	// There is currently no paginator or Marker/NextToken
	input := &ec2.DescribeAddressesInput{}

	output, err := conn.DescribeAddresses(input)

	if acctest.SkipSweepError(err) {
		log.Printf("[WARN] Skipping EC2 EIP sweep for %s: %s", region, err)
		return nil
	}

	if err != nil {
		return fmt.Errorf("error describing EC2 EIPs: %s", err)
	}

	if output == nil || len(output.Addresses) == 0 {
		log.Print("[DEBUG] No EC2 EIPs to sweep")
		return nil
	}

	sweepResources := make([]*acctest.SweepResource, 0)
	var errs *multierror.Error

	for _, address := range output.Addresses {
		publicIP := aws.StringValue(address.PublicIp)

		if address.AssociationId != nil {
			log.Printf("[INFO] Skipping EC2 EIP (%s) with association: %s", publicIP, aws.StringValue(address.AssociationId))
			continue
		}

		r := ResourceEIP()
		d := r.Data(nil)
		if address.AllocationId != nil {
			d.SetId(aws.StringValue(address.AllocationId))
		} else {
			d.SetId(aws.StringValue(address.PublicIp))
		}

		sweepResources = append(sweepResources, acctest.NewSweepResource(r, d, client))
	}

	if err = acctest.SweepOrchestrator(sweepResources); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("error sweeping EC2 EIPs for %s: %w", region, err))
	}

	if acctest.SkipSweepError(errs.ErrorOrNil()) {
		log.Printf("[WARN] Skipping EC2 EIP sweep for %s: %s", region, errs)
		return nil
	}

	return errs.ErrorOrNil()
}

func TestAccAWSEIP_basic(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					testAccCheckAWSEIPPublicDNS(resourceName),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_disappears(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					acctest.CheckResourceDisappears(acctest.Provider, ResourceEIP(), resourceName),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccAWSEIP_Instance(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEIPInstanceConfig(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Regression test for https://github.com/hashicorp/terraform/issues/3429 (now
// https://github.com/hashicorp/terraform-provider-aws/issues/42)
func TestAccAWSEIP_Instance_reassociate(t *testing.T) {
	instanceResourceName := "aws_instance.test"
	resourceName := "aws_eip.test"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPInstanceReassociateConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(resourceName, "instance", instanceResourceName, "id"),
				),
			},
			{
				Config: testAccEIPInstanceReassociateConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(resourceName, "instance", instanceResourceName, "id"),
				),
				Taint: []string{resourceName},
			},
		},
	})
}

// This test is an expansion of TestAccAWSEIP_instance, by testing the
// associated Private EIPs of two instances
func TestAccAWSEIP_Instance_associatedUserPrivateIP(t *testing.T) {
	var one ec2.Address
	resourceName := "aws_eip.test"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPInstanceAssociatedConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &one),
					testAccCheckAWSEIPAttributes(&one),
					testAccCheckAWSEIPAssociated(&one),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"associate_with_private_ip"},
			},
			{
				Config: testAccEIPInstanceAssociatedSwitchConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &one),
					testAccCheckAWSEIPAttributes(&one),
					testAccCheckAWSEIPAssociated(&one),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
				),
			},
		},
	})
}

func TestAccAWSEIP_Instance_notAssociated(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPInstanceAssociateNotAssociatedConfig(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccEIPInstanceAssociateAssociatedConfig(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					testAccCheckAWSEIPAssociated(&conf),
				),
			},
		},
	})
}

func TestAccAWSEIP_Instance_ec2Classic(t *testing.T) {
	resourceName := "aws_eip.test"
	var conf ec2.Address

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { acctest.PreCheck(t); acctest.PreCheckEC2Classic(t) },
		ErrorCheck:        acctest.ErrorCheck(t, ec2.EndpointsID),
		ProviderFactories: acctest.ProviderFactories,
		CheckDestroy:      testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPInstanceEC2ClassicConfig(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, true, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					testAccCheckAWSEIPPublicDNSEC2Classic(resourceName),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeStandard),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_NetworkInterface(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPNetworkInterfaceConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					testAccCheckAWSEIPAssociated(&conf),
					testAccCheckAWSEIPPrivateDNS(resourceName),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_NetworkInterface_twoEIPsOneInterface(t *testing.T) {
	var one, two ec2.Address
	resourceName := "aws_eip.test"
	resourceName2 := "aws_eip.test2"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPMultiNetworkInterfaceConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &one),
					testAccCheckAWSEIPAttributes(&one),
					testAccCheckAWSEIPAssociated(&one),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),

					testAccCheckAWSEIPExists(resourceName2, false, &two),
					testAccCheckAWSEIPAttributes(&two),
					testAccCheckAWSEIPAssociated(&two),
					resource.TestCheckResourceAttr(resourceName2, "domain", ec2.DomainTypeVpc),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"associate_with_private_ip"},
			},
		},
	})
}

func TestAccAWSEIP_Tags_EC2VPC_withVPCTrue(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	rName2 := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t); acctest.PreCheckEC2VPCOnly(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPTagsEC2VPCConfig(rName, "vpc = true"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.RandomName", rName),
					resource.TestCheckResourceAttr(resourceName, "tags.TestName", rName),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccEIPTagsEC2VPCConfig(rName2, "vpc = true"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.RandomName", rName2),
					resource.TestCheckResourceAttr(resourceName, "tags.TestName", rName2),
				),
			},
		},
	})
}

// Regression test for https://github.com/hashicorp/terraform-provider-aws/issues/18756
func TestAccAWSEIP_Tags_EC2VPC_withoutVPCTrue(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	rName2 := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t); acctest.PreCheckEC2VPCOnly(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPTagsEC2VPCConfig(rName, ""),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.RandomName", rName),
					resource.TestCheckResourceAttr(resourceName, "tags.TestName", rName),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccEIPTagsEC2VPCConfig(rName2, ""),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.RandomName", rName2),
					resource.TestCheckResourceAttr(resourceName, "tags.TestName", rName2),
				),
			},
		},
	})
}

func TestAccAWSEIP_Tags_EC2Classic_withVPCTrue(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	rName2 := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { acctest.PreCheck(t); acctest.PreCheckEC2Classic(t) },
		ErrorCheck:        acctest.ErrorCheck(t, ec2.EndpointsID),
		ProviderFactories: acctest.ProviderFactories,
		CheckDestroy:      testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPTagsEC2ClassicConfig(rName, "vpc = true"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, true, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.RandomName", rName),
					resource.TestCheckResourceAttr(resourceName, "tags.TestName", rName),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccEIPTagsEC2ClassicConfig(rName2, "vpc = true"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, true, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "tags.RandomName", rName2),
					resource.TestCheckResourceAttr(resourceName, "tags.TestName", rName2),
				),
			},
		},
	})
}

func TestAccAWSEIP_Tags_EC2Classic_withoutVPCTrue(t *testing.T) {
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { acctest.PreCheck(t); acctest.PreCheckEC2Classic(t) },
		ErrorCheck:        acctest.ErrorCheck(t, ec2.EndpointsID),
		ProviderFactories: acctest.ProviderFactories,
		CheckDestroy:      testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccEIPTagsEC2ClassicConfig(rName, ""),
				ExpectError: regexp.MustCompile(`tags cannot be set for a standard-domain EIP - must be a VPC-domain EIP`),
			},
		},
	})
}

func TestAccAWSEIP_PublicIPv4Pool_default(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPPublicIPv4PoolDefaultConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "public_ipv4_pool", "amazon"),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeVpc),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_PublicIPv4Pool_custom(t *testing.T) {
	if os.Getenv("AWS_EC2_EIP_PUBLIC_IPV4_POOL") == "" {
		t.Skip("Environment variable AWS_EC2_EIP_PUBLIC_IPV4_POOL is not set")
	}

	var conf ec2.Address
	resourceName := "aws_eip.test"

	poolName := os.Getenv("AWS_EC2_EIP_PUBLIC_IPV4_POOL")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPPublicIPv4PoolCustomConfig(poolName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "public_ipv4_pool", poolName),
					resource.TestCheckResourceAttr(resourceName, "domain", ec2.DomainTypeStandard),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_CustomerOwnedIPv4Pool(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t); acctest.PreCheckOutpostsOutposts(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPCustomerOwnedIPv4PoolConfig(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					resource.TestMatchResourceAttr(resourceName, "customer_owned_ipv4_pool", regexp.MustCompile(`^ipv4pool-coip-.+$`)),
					resource.TestMatchResourceAttr(resourceName, "customer_owned_ip", regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`)),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_networkBorderGroup(t *testing.T) {
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPNetworkBorderGroupConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "public_ipv4_pool", "amazon"),
					resource.TestCheckResourceAttr(resourceName, "network_border_group", acctest.Region()),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_carrierIP(t *testing.T) {
	var conf ec2.Address
	rName := sdkacctest.RandomWithPrefix("tf-acc-test")
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t); testAccPreCheckAWSWavelengthZoneAvailable(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEIPCarrierIPConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					resource.TestCheckResourceAttrSet(resourceName, "carrier_ip"),
					resource.TestCheckResourceAttrSet(resourceName, "network_border_group"),
					resource.TestCheckResourceAttr(resourceName, "public_ip", ""),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSEIP_BYOIPAddress_default(t *testing.T) {
	// Test case address not set
	var conf ec2.Address
	resourceName := "aws_eip.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEIPConfig_BYOIPAddress_custom_default,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
				),
			},
		},
	})
}

func TestAccAWSEIP_BYOIPAddress_custom(t *testing.T) {
	// Test Case for address being set

	if os.Getenv("AWS_EC2_EIP_BYOIP_ADDRESS") == "" {
		t.Skip("Environment variable AWS_EC2_EIP_BYOIP_ADDRESS is not set")
	}

	var conf ec2.Address
	resourceName := "aws_eip.test"

	address := os.Getenv("AWS_EC2_EIP_BYOIP_ADDRESS")

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEIPConfig_BYOIPAddress_custom(address),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "public_ip", address),
				),
			},
		},
	})
}

func TestAccAWSEIP_BYOIPAddress_custom_with_PublicIpv4Pool(t *testing.T) {
	// Test Case for both address and public_ipv4_pool being set
	if os.Getenv("AWS_EC2_EIP_BYOIP_ADDRESS") == "" {
		t.Skip("Environment variable AWS_EC2_EIP_BYOIP_ADDRESS is not set")
	}

	if os.Getenv("AWS_EC2_EIP_PUBLIC_IPV4_POOL") == "" {
		t.Skip("Environment variable AWS_EC2_EIP_PUBLIC_IPV4_POOL is not set")
	}

	var conf ec2.Address
	resourceName := "aws_eip.test"

	address := os.Getenv("AWS_EC2_EIP_BYOIP_ADDRESS")
	poolName := os.Getenv("AWS_EC2_EIP_PUBLIC_IPV4_POOL")

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckAWSEIPDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSEIPConfig_BYOIPAddress_custom_with_PublicIpv4Pool(address, poolName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSEIPExists(resourceName, false, &conf),
					testAccCheckAWSEIPAttributes(&conf),
					resource.TestCheckResourceAttr(resourceName, "public_ip", address),
					resource.TestCheckResourceAttr(resourceName, "public_ipv4_pool", poolName),
				),
			},
		},
	})
}

func testAccCheckAWSEIPDestroy(s *terraform.State) error {
	conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_eip" {
			continue
		}

		if strings.Contains(rs.Primary.ID, "eipalloc") {
			req := &ec2.DescribeAddressesInput{
				AllocationIds: []*string{aws.String(rs.Primary.ID)},
			}
			describe, err := conn.DescribeAddresses(req)
			if err != nil {
				// Verify the error is what we want
				if ae, ok := err.(awserr.Error); ok && ae.Code() == "InvalidAllocationID.NotFound" || ae.Code() == "InvalidAddress.NotFound" {
					continue
				}
				return err
			}

			if len(describe.Addresses) > 0 {
				return fmt.Errorf("still exists")
			}
		} else {
			req := &ec2.DescribeAddressesInput{
				PublicIps: []*string{aws.String(rs.Primary.ID)},
			}
			describe, err := conn.DescribeAddresses(req)
			if err != nil {
				// Verify the error is what we want
				if ae, ok := err.(awserr.Error); ok && ae.Code() == "InvalidAllocationID.NotFound" || ae.Code() == "InvalidAddress.NotFound" {
					continue
				}
				return err
			}

			if len(describe.Addresses) > 0 {
				return fmt.Errorf("still exists")
			}
		}
	}

	return nil
}

func testAccCheckAWSEIPAttributes(conf *ec2.Address) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *conf.PublicIp == "" {
			return fmt.Errorf("empty public_ip")
		}

		return nil
	}
}

func testAccCheckAWSEIPAssociated(conf *ec2.Address) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if conf.AssociationId == nil || *conf.AssociationId == "" {
			return fmt.Errorf("empty association_id")
		}

		return nil
	}
}

func testAccCheckAWSEIPExists(n string, ec2classic bool, res *ec2.Address) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No EIP ID is set")
		}

		conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn

		if ec2classic {
			conn = acctest.ProviderEC2Classic.Meta().(*conns.AWSClient).EC2Conn
		}

		input := &ec2.DescribeAddressesInput{}

		if strings.Contains(rs.Primary.ID, "eipalloc") {
			input.AllocationIds = aws.StringSlice([]string{rs.Primary.ID})
		} else {
			input.PublicIps = aws.StringSlice([]string{rs.Primary.ID})
		}

		var output *ec2.DescribeAddressesOutput

		err := resource.Retry(15*time.Minute, func() *resource.RetryError {
			var err error

			output, err = conn.DescribeAddresses(input)

			if tfawserr.ErrCodeEquals(err, "InvalidAllocationID.NotFound") {
				return resource.RetryableError(err)
			}

			if tfawserr.ErrCodeEquals(err, "InvalidAddress.NotFound") {
				return resource.RetryableError(err)
			}

			if err != nil {
				return resource.NonRetryableError(err)
			}

			return nil
		})

		if tfresource.TimedOut(err) {
			output, err = conn.DescribeAddresses(input)
		}

		if err != nil {
			return fmt.Errorf("while describing addresses (%s): %w", rs.Primary.ID, err)
		}

		if len(output.Addresses) != 1 {
			return fmt.Errorf("wrong number of EIP found for (%s): %d", rs.Primary.ID, len(output.Addresses))
		}

		if aws.StringValue(output.Addresses[0].AllocationId) != rs.Primary.ID && aws.StringValue(output.Addresses[0].PublicIp) != rs.Primary.ID {
			return fmt.Errorf("EIP (%s) not found", rs.Primary.ID)
		}

		*res = *output.Addresses[0]

		return nil
	}
}

func testAccCheckAWSEIPPrivateDNS(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Not found: %s", resourceName)
		}

		privateDNS := rs.Primary.Attributes["private_dns"]
		expectedPrivateDNS := fmt.Sprintf(
			"ip-%s.%s",
			convertIPToDashIP(rs.Primary.Attributes["private_ip"]),
			regionalPrivateDNSSuffix(acctest.Region()),
		)

		if privateDNS != expectedPrivateDNS {
			return fmt.Errorf("expected private_dns value (%s), received: %s", expectedPrivateDNS, privateDNS)
		}

		return nil
	}
}

func testAccCheckAWSEIPPublicDNS(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Not found: %s", resourceName)
		}

		publicDNS := rs.Primary.Attributes["public_dns"]
		expectedPublicDNS := fmt.Sprintf(
			"ec2-%s.%s.%s",
			convertIPToDashIP(rs.Primary.Attributes["public_ip"]),
			resourceAwsEc2RegionalPublicDnsSuffix(acctest.Region()),
			acctest.PartitionDNSSuffix(),
		)

		if publicDNS != expectedPublicDNS {
			return fmt.Errorf("expected public_dns value (%s), received: %s", expectedPublicDNS, publicDNS)
		}

		return nil
	}
}

func testAccCheckAWSEIPPublicDNSEC2Classic(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Not found: %s", resourceName)
		}

		publicDNS := rs.Primary.Attributes["public_dns"]
		expectedPublicDNS := fmt.Sprintf(
			"ec2-%s.%s.%s",
			convertIPToDashIP(rs.Primary.Attributes["public_ip"]),
			resourceAwsEc2RegionalPublicDnsSuffix(acctest.EC2ClassicRegion()),
			acctest.PartitionDNSSuffix(),
		)

		if publicDNS != expectedPublicDNS {
			return fmt.Errorf("expected public_dns value (%s), received: %s", expectedPublicDNS, publicDNS)
		}

		return nil
	}
}

const testAccEIPConfig = `
resource "aws_eip" "test" {
  vpc = true
}
`

func testAccEIPTagsEC2VPCConfig(rName, vpcConfig string) string {
	return fmt.Sprintf(`
resource "aws_eip" "test" {
  %[1]s

  tags = {
    RandomName = %[2]q
    TestName   = %[2]q
  }
}
`, vpcConfig, rName)
}

func testAccEIPTagsEC2ClassicConfig(rName, vpcConfig string) string {
	return acctest.ConfigCompose(
		acctest.ConfigEC2ClassicRegionProvider(),
		fmt.Sprintf(`
resource "aws_eip" "test" {
  %[1]s

  tags = {
    RandomName = %[2]q
    TestName   = %[2]q
  }
}
`, vpcConfig, rName))
}

const testAccEIPPublicIPv4PoolDefaultConfig = `
resource "aws_eip" "test" {
  vpc = true
}
`

func testAccEIPPublicIPv4PoolCustomConfig(poolName string) string {
	return fmt.Sprintf(`
resource "aws_eip" "test" {
  vpc              = true
  public_ipv4_pool = %[1]q
}
`, poolName)
}

func testAccEIPInstanceEC2ClassicConfig() string {
	return acctest.ConfigCompose(
		acctest.ConfigEC2ClassicRegionProvider(),
		testAccLatestAmazonLinuxPvEbsAmiConfig(),
		acctest.AvailableEC2InstanceTypeForRegion("t1.micro", "m3.medium", "m3.large", "c3.large", "r3.large"),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-pv-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
}

resource "aws_eip" "test" {
  instance = aws_instance.test.id
}
`)
}

const testAccAWSEIPConfig_BYOIPAddress_custom_default = `
resource "aws_eip" "test" {
  vpc = true
}
`

func testAccAWSEIPConfig_BYOIPAddress_custom(address string) string {
	return fmt.Sprintf(`
resource "aws_eip" "test" {
  vpc     = true
  address = %[1]q
}
`, address)
}

func testAccAWSEIPConfig_BYOIPAddress_custom_with_PublicIpv4Pool(address string, poolname string) string {
	return fmt.Sprintf(`
resource "aws_eip" "test" {
  vpc              = true
  address          = %[1]q
  public_ipv4_pool = %[2]q
}
`, address, poolname)
}

func testAccAWSEIPInstanceConfig() string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHVMEBSAMI(),
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("aws_subnet.test.availability_zone", "t3.micro", "t2.micro"),
		acctest.ConfigAvailableAZsNoOptIn(),
		`
resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_subnet" "test" {
  availability_zone = data.aws_availability_zones.available.names[0]
  cidr_block        = cidrsubnet(aws_vpc.test.cidr_block, 8, 0)
  vpc_id            = aws_vpc.test.id
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id     = aws_subnet.test.id
}

resource "aws_eip" "test" {
  instance = aws_instance.test.id
  vpc      = true
}
`)
}

func testAccEIPInstanceAssociatedConfig(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHVMEBSAMI(),
		acctest.ConfigAvailableAZsNoOptIn(),
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("aws_subnet.test.availability_zone", "t3.micro", "t2.micro"),
		fmt.Sprintf(`
resource "aws_vpc" "default" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true

  tags = {
    Name = %[1]q
  }
}

resource "aws_internet_gateway" "gw" {
  vpc_id = aws_vpc.default.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_subnet" "test" {
  vpc_id                  = aws_vpc.default.id
  availability_zone       = data.aws_availability_zones.available.names[0]
  cidr_block              = "10.0.0.0/24"
  map_public_ip_on_launch = true

  depends_on = [aws_internet_gateway.gw]

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  private_ip = "10.0.0.12"
  subnet_id  = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test2" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  private_ip = "10.0.0.19"
  subnet_id  = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_eip" "test" {
  vpc = true

  instance                  = aws_instance.test2.id
  associate_with_private_ip = "10.0.0.19"
}
`, rName))
}

func testAccEIPInstanceAssociatedSwitchConfig(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHVMEBSAMI(),
		fmt.Sprintf(`
resource "aws_vpc" "default" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true

  tags = {
    Name = %[1]q
  }
}

resource "aws_internet_gateway" "gw" {
  vpc_id = aws_vpc.default.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_subnet" "test" {
  vpc_id                  = aws_vpc.default.id
  cidr_block              = "10.0.0.0/24"
  map_public_ip_on_launch = true

  depends_on = [aws_internet_gateway.gw]

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"

  private_ip = "10.0.0.12"
  subnet_id  = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test2" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.micro"

  private_ip = "10.0.0.19"
  subnet_id  = aws_subnet.test.id

  tags = {
    Name = "%[1]s-2"
  }
}

resource "aws_eip" "test" {
  vpc = true

  instance                  = aws_instance.test.id
  associate_with_private_ip = "10.0.0.12"
}
`, rName))
}

func testAccEIPNetworkInterfaceConfig(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigAvailableAZsNoOptIn(),
		fmt.Sprintf(`
resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/24"
  tags = {
    Name = %[1]q
  }
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id
}

resource "aws_subnet" "test" {
  vpc_id            = aws_vpc.test.id
  availability_zone = data.aws_availability_zones.available.names[0]
  cidr_block        = "10.0.0.0/24"
  tags = {
    Name = %[1]q
  }
}

resource "aws_network_interface" "test" {
  subnet_id       = aws_subnet.test.id
  private_ips     = ["10.0.0.10"]
  security_groups = [aws_vpc.test.default_security_group_id]
}

resource "aws_eip" "test" {
  vpc               = "true"
  network_interface = aws_network_interface.test.id
  depends_on        = [aws_internet_gateway.test]
}
`, rName))
}

func testAccEIPMultiNetworkInterfaceConfig(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigAvailableAZsNoOptIn(),
		fmt.Sprintf(`
resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/24"
  tags = {
    Name = %[1]q
  }
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id
}

resource "aws_subnet" "test" {
  vpc_id            = aws_vpc.test.id
  availability_zone = data.aws_availability_zones.available.names[0]
  cidr_block        = "10.0.0.0/24"
  tags = {
    Name = %[1]q
  }
}

resource "aws_network_interface" "test" {
  subnet_id       = aws_subnet.test.id
  private_ips     = ["10.0.0.10", "10.0.0.11"]
  security_groups = [aws_vpc.test.default_security_group_id]
}

resource "aws_eip" "test" {
  vpc                       = "true"
  network_interface         = aws_network_interface.test.id
  associate_with_private_ip = "10.0.0.10"
  depends_on                = [aws_internet_gateway.test]
}

resource "aws_eip" "test2" {
  vpc                       = "true"
  network_interface         = aws_network_interface.test.id
  associate_with_private_ip = "10.0.0.11"
  depends_on                = [aws_internet_gateway.test]
}
`, rName))
}

func testAccEIPInstanceReassociateConfig(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHVMEBSAMI(),
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("aws_subnet.test.availability_zone", "t3.micro", "t2.micro"),
		fmt.Sprintf(`
resource "aws_eip" "test" {
  instance = aws_instance.test.id
  vpc      = true

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  associate_public_ip_address = true
  instance_type               = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id                   = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/16"

  tags = {
    Name = %[1]q
  }
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_subnet" "test" {
  cidr_block = "10.0.0.0/24"
  vpc_id     = aws_vpc.test.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_route_table" "test" {
  vpc_id = aws_vpc.test.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.test.id
  }

  tags = {
    Name = %[1]q
  }
}

resource "aws_route_table_association" "test" {
  subnet_id      = aws_subnet.test.id
  route_table_id = aws_route_table.test.id
}
`, rName))
}

func testAccEIPInstanceAssociateNotAssociatedConfig() string {
	return acctest.ConfigCompose(
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("aws_subnet.test.availability_zone", "t3.micro", "t2.micro"),
		acctest.ConfigAvailableAZsNoOptIn(),
		acctest.ConfigLatestAmazonLinuxHVMEBSAMI(), `
resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_subnet" "test" {
  availability_zone = data.aws_availability_zones.available.names[0]
  cidr_block        = cidrsubnet(aws_vpc.test.cidr_block, 8, 0)
  vpc_id            = aws_vpc.test.id
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id     = aws_subnet.test.id
}

resource "aws_eip" "test" {
}
`)
}

func testAccEIPInstanceAssociateAssociatedConfig() string {
	return acctest.ConfigCompose(
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("aws_subnet.test.availability_zone", "t3.micro", "t2.micro"),
		acctest.ConfigAvailableAZsNoOptIn(),
		acctest.ConfigLatestAmazonLinuxHVMEBSAMI(), `
resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_subnet" "test" {
  availability_zone = data.aws_availability_zones.available.names[0]
  cidr_block        = cidrsubnet(aws_vpc.test.cidr_block, 8, 0)
  vpc_id            = aws_vpc.test.id
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id     = aws_subnet.test.id
}

resource "aws_eip" "test" {
  instance = aws_instance.test.id
  vpc      = true
}
`)
}

func testAccEIPCustomerOwnedIPv4PoolConfig() string {
	return `
data "aws_ec2_coip_pools" "test" {}

resource "aws_eip" "test" {
  customer_owned_ipv4_pool = tolist(data.aws_ec2_coip_pools.test.pool_ids)[0]
  vpc                      = true
}
`
}

const testAccEIPNetworkBorderGroupConfig = `
data "aws_region" current {}

resource "aws_eip" "test" {
  vpc                  = true
  network_border_group = data.aws_region.current.name
}
`

func testAccEIPCarrierIPConfig(rName string) string {
	return acctest.ConfigCompose(
		testAccAvailableAZsWavelengthZonesDefaultExcludeConfig(),
		fmt.Sprintf(`
data "aws_availability_zone" "available" {
  name = data.aws_availability_zones.available.names[0]
}

resource "aws_eip" "test" {
  vpc                  = true
  network_border_group = data.aws_availability_zone.available.network_border_group

  tags = {
    Name = %[1]q
  }
}
`, rName))
}