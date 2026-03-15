// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

/**
 * @title FloeCreditDEX
 * @dev A P2P Intent-Matching Decentralized Exchange for Credit.
 * Allows autonomous AI agents to post borrowing and lending intents,
 * matching them on-chain with automatic USDC escrow and repayment enforcement.
 */
contract FloeCreditDEX is ReentrancyGuard, Ownable {
    using SafeERC20 for IERC20;

    IERC20 public immutable usdc;

    struct CreditIntent {
        address creator;
        uint256 amount;
        uint256 interestRateBps; // Basis points (e.g., 500 = 5%)
        uint256 durationSec;
        bool isLend; // True if lending, false if borrowing
        bool active;
    }

    struct Loan {
        address lender;
        address borrower;
        uint256 principal;
        uint256 interest;
        uint256 deadline;
        bool repaid;
        bool defaulted;
    }

    mapping(bytes32 => CreditIntent) public intents;
    mapping(bytes32 => Loan) public loans;

    event IntentCreated(bytes32 indexed intentId, address indexed creator, bool isLend, uint256 amount, uint256 interestRateBps);
    event IntentCanceled(bytes32 indexed intentId);
    event LoanInitiated(bytes32 indexed loanId, bytes32 indexed intentId, address lender, address borrower, uint256 principal);
    event LoanRepaid(bytes32 indexed loanId, uint256 totalRepaid);
    event LoanDefaulted(bytes32 indexed loanId);

    constructor(address _usdc) Ownable(msg.sender) {
        require(_usdc != address(0), "Invalid USDC address");
        usdc = IERC20(_usdc);
    }

    /**
     * @dev Agents call this to post an intent to the orderbook.
     * If lending, USDC must be approved and is escrowed immediately.
     */
    function postIntent(
        uint256 amount,
        uint256 interestRateBps,
        uint256 durationSec,
        bool isLend
    ) external nonReentrant returns (bytes32 intentId) {
        require(amount > 0, "Amount must be > 0");
        require(durationSec > 0, "Duration must be > 0");

        intentId = keccak256(abi.encodePacked(msg.sender, amount, interestRateBps, durationSec, isLend, block.timestamp));
        require(!intents[intentId].active, "Intent colission");

        if (isLend) {
            usdc.safeTransferFrom(msg.sender, address(this), amount);
        }

        intents[intentId] = CreditIntent({
            creator: msg.sender,
            amount: amount,
            interestRateBps: interestRateBps,
            durationSec: durationSec,
            isLend: isLend,
            active: true
        });

        emit IntentCreated(intentId, msg.sender, isLend, amount, interestRateBps);
    }

    /**
     * @dev Cancel an active intent. Refunda escrowed USDC if lending.
     */
    function cancelIntent(bytes32 intentId) external nonReentrant {
        CreditIntent storage intent = intents[intentId];
        require(intent.active, "Intent not active");
        require(intent.creator == msg.sender || msg.sender == owner(), "Unauthorized");

        intent.active = false;

        if (intent.isLend) {
            usdc.safeTransfer(intent.creator, intent.amount);
        }

        emit IntentCanceled(intentId);
    }

    /**
     * @dev Match an existing intent.
     * If matching a 'Borrow' intent, the caller provides USDC contextually.
     * If matching a 'Lend' intent, the escrowed USDC is released to the caller.
     */
    function matchIntent(bytes32 intentId) external nonReentrant returns (bytes32 loanId) {
        CreditIntent storage intent = intents[intentId];
        require(intent.active, "Intent not active");
        require(intent.creator != msg.sender, "Cannot match own intent");

        intent.active = false;

        address lender;
        address borrower;

        if (intent.isLend) {
            lender = intent.creator;
            borrower = msg.sender;
            usdc.safeTransfer(borrower, intent.amount);
        } else {
            lender = msg.sender;
            borrower = intent.creator;
            usdc.safeTransferFrom(lender, borrower, intent.amount);
        }

        uint256 interest = (intent.amount * intent.interestRateBps) / 10000;
        
        loanId = keccak256(abi.encodePacked(intentId, block.timestamp));
        loans[loanId] = Loan({
            lender: lender,
            borrower: borrower,
            principal: intent.amount,
            interest: interest,
            deadline: block.timestamp + intent.durationSec,
            repaid: false,
            defaulted: false
        });

        emit LoanInitiated(loanId, intentId, lender, borrower, intent.amount);
    }

    /**
     * @dev Borrower repays the loan (Principal + Interest).
     */
    function repayLoan(bytes32 loanId) external nonReentrant {
        Loan storage loan = loans[loanId];
        require(!loan.repaid, "Already repaid");
        require(!loan.defaulted, "Loan defaulted");
        
        uint256 totalDue = loan.principal + loan.interest;
        loan.repaid = true;

        usdc.safeTransferFrom(msg.sender, loan.lender, totalDue);

        emit LoanRepaid(loanId, totalDue);
    }

    /**
     * @dev Lender marks loan as defaulted to slash borrower reputation/collateral (external).
     */
    function markDefault(bytes32 loanId) external {
        Loan storage loan = loans[loanId];
        require(!loan.repaid, "Loan repaid");
        require(!loan.defaulted, "Already defaulted");
        require(block.timestamp > loan.deadline, "Not past deadline");
        // Optimization: Not strictly restricting to lender to allow keepers to flag.

        loan.defaulted = true;
        emit LoanDefaulted(loanId);
    }
}
