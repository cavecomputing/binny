"""JSON API, mounted at /api. Each resource lives in its own child blueprint."""
import logging
import os

from flask import Blueprint, jsonify, request
from werkzeug.exceptions import HTTPException

from . import files, folders, tags, trash

logger = logging.getLogger(__name__)

bp = Blueprint('api', __name__, url_prefix='/api')
for module in (files, folders, tags, trash):
    bp.register_blueprint(module.bp)


@bp.errorhandler(HTTPException)
def http_error(e):
    """abort(status, message) -> {"error": message} with that status."""
    return jsonify({'error': e.description}), e.code


@bp.errorhandler(OSError)
def disk_error(e):
    """A disk operation that failed, e.g. a permission problem: say what the disk said."""
    logger.warning('%s failed: %s', request.endpoint, e)
    name = f' ("{os.path.basename(e.filename)}")' if e.filename else ''
    return jsonify({'error': f'{e.strerror or "The disk refused"}{name}'}), 500


@bp.errorhandler(Exception)
def unexpected_error(e):
    logger.exception('Unhandled error in %s', request.endpoint)
    return jsonify({'error': 'Something went wrong on the server'}), 500
