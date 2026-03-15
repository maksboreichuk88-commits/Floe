// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test, console} from "forge-std/Test.sol";
import {FloeCreditDEX} from "../src/FloeCreditDEX.sol";
import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract MockUSDC is ERC20 {
    constructor() ERC20("Mock USDC", "USDC") {
        _mint(msg.sender, 1000000 * 10**6);
    }
    
    function decimals() public view virtual override returns (uint8) {
        return 6;
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}

contract FloeCreditDEXTest is Test {
    FloeCreditDEX public dex;
    MockUSDC public usdc;

    address alice = address(0x1);
    address bob = address(0x2);

    function setUp() public {
        usdc = new MockUSDC();
        dex = new FloeCreditDEX(address(usdc));

        usdc.mint(alice, 10000 * 10**6);
        usdc.mint(bob, 10000 * 10**6);

        vm.startPrank(alice);
        usdc.approve(address(dex), type(uint256).max);
        vm.stopPrank();

        vm.startPrank(bob);
        usdc.approve(address(dex), type(uint256).max);
        vm.stopPrank();
    }

    function testPostLendIntent() public {
        uint256 amount = 1000 * 10**6;
        uint256 interest = 500; // 5%
        uint256 duration = 30 days;

        vm.prank(alice);
        bytes32 intentId = dex.postIntent(amount, interest, duration, true);

        // Verify state
        (address creator, uint256 amt, uint256 rate, uint256 dur, bool isLend, bool active) = dex.intents(intentId);
        assertEq(creator, alice);
        assertEq(amt, amount);
        assertEq(rate, interest);
        assertEq(dur, duration);
        assertTrue(isLend);
        assertTrue(active);

        // Verify Escrow
        assertEq(usdc.balanceOf(address(dex)), amount);
    }

    function testMatchLendIntent() public {
        uint256 amount = 1000 * 10**6;
        
        vm.prank(alice);
        bytes32 intentId = dex.postIntent(amount, 500, 30 days, true);

        uint256 bobBalBefore = usdc.balanceOf(bob);

        vm.prank(bob);
        bytes32 loanId = dex.matchIntent(intentId);

        // Bob should receive Alice's USDC
        assertEq(usdc.balanceOf(bob), bobBalBefore + amount);

        (,,,,,,bool repaid) = dex.loans(loanId);
        assertFalse(repaid);
    }

    function testRepayLoan() public {
        uint256 amount = 1000 * 10**6;
        
        vm.prank(alice);
        bytes32 intentId = dex.postIntent(amount, 500, 30 days, true);

        vm.prank(bob);
        bytes32 loanId = dex.matchIntent(intentId);

        // 5% interest on 1000 = 50
        uint256 totalDue = 1050 * 10**6;
        uint256 aliceBalBefore = usdc.balanceOf(alice);

        vm.prank(bob);
        dex.repayLoan(loanId);

        assertEq(usdc.balanceOf(alice), aliceBalBefore + totalDue);
    }

    function testCancelIntentRevertsIfAlreadyMatched() public {
        uint256 amount = 1000 * 10**6;
        
        vm.prank(alice);
        bytes32 intentId = dex.postIntent(amount, 500, 30 days, true);

        vm.prank(bob);
        dex.matchIntent(intentId);

        vm.expectRevert("Intent not active");
        vm.prank(alice);
        dex.cancelIntent(intentId);
    }
}
